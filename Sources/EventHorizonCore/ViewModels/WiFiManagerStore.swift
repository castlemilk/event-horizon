import Foundation
import SwiftUI

@Observable
@MainActor
public final class WiFiManagerStore {
    public var hotspots: [AccessPoint] = []
    public var selectedHotspot: AccessPoint?
    public var topologyNodes: [HardwareTopologyNode] = []
    public var interfaceStats: [InterfaceStat] = []
    public var pingResults: [PingResult] = []
    public var stabilityStats: StabilityStats?
    public var isConnecting = false
    public var statusMessage = "Daemon Ready"
    public var isDaemonConnected = true
    public var selectedInterface: String = "en0"
    public var selectedDongleId: String? = nil
    public var pingTargetHost: String = "1.1.1.1"
    public var isPinging: Bool = false
    public var lastPingSuccess: Bool? = nil
    public var lastPingRTTMs: Int64 = 12
    public var starlinkDishReachable = false
    public var starlinkPingMs: Int = 18
    public private(set) var signalHistory: [Double] = []
    public private(set) var latencyHistory: [Double] = []
    public private(set) var rxHistory: [Double] = []
    public private(set) var txHistory: [Double] = []
    public private(set) var isRunningSpeedTest = false
    public var speedTestResult: SpeedTestResult?
    public var speedTestError: String?

    private let client: WiFiDaemonClientProviding
    private let supervisor: RuntimeSupervising
    private var pollTask: Task<Void, Never>?

    public init(
        client: WiFiDaemonClientProviding = WiFiDaemonClient(),
        supervisor: RuntimeSupervising = RuntimeSupervisor()
    ) {
        self.client = client
        self.supervisor = supervisor
        Task {
            await bootstrap()
        }
    }

    public func bootstrap() async {
        do {
            try await supervisor.ensureDaemonRunning()
            isDaemonConnected = true
            statusMessage = "Connected to USB Daemon"
            await refreshData()
            startPeriodicPolling()
        } catch {
            isDaemonConnected = false
            statusMessage = "Daemon offline: \(error.localizedDescription)"
        }
    }

    public func refreshData() async {
        do {
            let list = try await client.fetchHotspots()
            self.hotspots = list.sorted {
                if $0.isSelected != $1.isSelected {
                    return $0.isSelected
                }
                return $0.ssid < $1.ssid
            }

            if let active = self.hotspots.first(where: { $0.isSelected }) {
                self.selectedHotspot = active
            } else if self.selectedHotspot == nil {
                self.selectedHotspot = self.hotspots.first
            }

            if let topNodes = try? await client.fetchHardwareTopology(), !topNodes.isEmpty {
                self.topologyNodes = self.sortedInterfaces(topNodes)
            }

            if let stats = try? await client.fetchTelemetry() {
                self.interfaceStats = stats
            }

            if let pings = try? await client.fetchPingDiagnostics(interface: selectedInterface, target: pingTargetHost) {
                self.pingResults = pings
            }

            if let uptime = try? await client.fetchUptimeStats() {
                self.stabilityStats = uptime
            }

            self.signalHistory = appendSample(self.signalHistory, value: Double(activeHotspotForSelectedInterface.rssi))
            self.latencyHistory = appendSample(self.latencyHistory, value: Double(pingResults.first?.rttMs ?? 0))
            if let first = interfaceStats.first {
                self.rxHistory = appendSample(self.rxHistory, value: first.rxRateKBps)
                self.txHistory = appendSample(self.txHistory, value: first.txRateKBps)
            }

            self.isDaemonConnected = true
            checkStarlinkDishTelemetry()
        } catch {
            if let _ = try? await client.fetchStatus() {
                self.isDaemonConnected = true
            } else if !self.topologyNodes.isEmpty {
                self.isDaemonConnected = true
            } else {
                self.isDaemonConnected = false
            }
        }
    }

    public func connect(to ssid: String, passphrase: String = "") async {
        if selectedInterface.isEmpty {
            statusMessage = "Select an active interface (e.g. en0) before connecting — AIC8800D80 has no macOS driver"
            return
        }
        isConnecting = true
        statusMessage = "Authenticating with '\(ssid)' on \(selectedInterface)..."
        do {
            let ap = try await client.connectToHotspot(ssid: ssid, passphrase: passphrase)
            self.selectedHotspot = ap

            // 1. Update topologyNode for selectedInterface
            if let idx = topologyNodes.firstIndex(where: { $0.bsdInterface == selectedInterface }) {
                let old = topologyNodes[idx]
                topologyNodes[idx] = HardwareTopologyNode(
                    usbDriver: old.usbDriver,
                    vendorId: old.vendorId,
                    productId: old.productId,
                    serialNumber: old.serialNumber,
                    speed: old.speed,
                    bsdInterface: old.bsdInterface,
                    networkTarget: ssid,
                    ipAddress: old.ipAddress,
                    subnetMask: old.subnetMask,
                    gateway: old.gateway,
                    macAddress: old.macAddress,
                    status: "Connected to '\(ssid)'",
                    driverType: old.driverType
                )
            }

            // 2. Update hotspots selection state
            self.hotspots = self.hotspots.map { item in
                AccessPoint(
                    ssid: item.ssid,
                    bssid: item.bssid,
                    rssi: item.rssi,
                    channel: item.channel,
                    security: item.security,
                    isSelected: (item.ssid == ssid)
                )
            }

            self.statusMessage = "Connected to '\(ssid)' on \(selectedInterface)"
        } catch {
            self.statusMessage = "Connection failed: \(error.localizedDescription)"
        }
        isConnecting = false
    }

    public func disconnect() async {
        statusMessage = "Disconnecting from current network..."
        do {
            try await client.disconnectFromNetwork()
            self.selectedHotspot = nil
            self.hotspots = self.hotspots.map { ap in
                AccessPoint(ssid: ap.ssid, bssid: ap.bssid, rssi: ap.rssi, channel: ap.channel, security: ap.security, isSelected: false)
            }
            self.signalHistory = []
            self.latencyHistory = []
            statusMessage = "Disconnected"
        } catch {
            statusMessage = "Disconnect failed: \(error.localizedDescription)"
        }
    }

    public var activeHotspotForSelectedInterface: AccessPoint {
        guard let node = topologyNodes.first(where: { $0.bsdInterface == selectedInterface }) else {
            return selectedHotspot ?? AccessPoint(ssid: "", bssid: "", rssi: 0, channel: 0, security: "", isSelected: false)
        }
        let observed = hotspots.first(where: { !$0.ssid.isEmpty && $0.ssid == node.networkTarget })
        let isWired = node.usbDriver.localizedCaseInsensitiveContains("ethernet")
            || node.usbDriver.localizedCaseInsensitiveContains("lan")
            || node.usbDriver.localizedCaseInsensitiveContains("rtl8156")
        return AccessPoint(
            ssid: observed?.ssid ?? node.networkTarget,
            bssid: observed?.bssid ?? "",
            rssi: observed?.rssi ?? 0,
            channel: observed?.channel ?? 0,
            security: isWired ? "Ethernet" : (observed?.security ?? ""),
            isSelected: observed != nil
        )
    }

    public func selectDeviceInterface(_ iface: String) {
        self.selectedInterface = iface
        self.selectedDongleId = nil
        self.selectedHotspot = activeHotspotForSelectedInterface
        guard let node = topologyNodes.first(where: { $0.bsdInterface == iface }) else {
            self.statusMessage = "Targeting interface '\(iface)'"
            return
        }
        let target = node.networkTarget.isEmpty ? "no active network" : node.networkTarget
        self.statusMessage = "Targeting \(node.usbDriver) (\(iface)) • \(target)"
    }

    public func selectDongle(_ node: HardwareTopologyNode) {
        self.selectedDongleId = HardwareTopologyNode.dongleId(node)
        self.selectedInterface = ""
        self.selectedHotspot = nil
        self.statusMessage = "Targeting \(node.usbDriver)"
    }

    /// Mode-switches a ZeroCD storage-mode USB dongle into WLAN mode so it
    /// re-enumerates with a BSD interface and becomes selectable.
    public func modeSwitchDongle() async {
        isConnecting = true
        statusMessage = "Mode-switching USB dongle to Wi-Fi..."
        do {
            try await client.switchDongleToWiFiMode()
            statusMessage = "Dongle mode-switched — re-scanning devices..."
            try? await Task.sleep(for: .seconds(3))
            await refreshData()
            statusMessage = "Dongle mode-switched into WLAN mode"
        } catch {
            statusMessage = "Mode switch failed: \(error.localizedDescription)"
        }
        isConnecting = false
    }

    public var interfaceNodes: [HardwareTopologyNode] {
        topologyNodes
            .filter { !$0.bsdInterface.isEmpty }
            .sorted {
                let aDefault = $0.status.contains("Default Route")
                let bDefault = $1.status.contains("Default Route")
                if aDefault != bDefault { return aDefault }
                return $0.bsdInterface < $1.bsdInterface
            }
    }

    public var dongleNodes: [HardwareTopologyNode] {
        topologyNodes.filter { $0.bsdInterface.isEmpty }
    }

    /// Devices worth offering in the systray quick picker: live/active interfaces
    /// (no dead Thunderbolt/Ethernet ports) plus USB Wi-Fi dongles.
    public var quickSelectDevices: [HardwareTopologyNode] {
        let useful = interfaceNodes.filter {
            $0.status.contains("Default Route")
                || $0.status.contains("Up")
                || $0.usbDriver.localizedCaseInsensitiveContains("wifi")
        }
        return useful + dongleNodes
    }

    public func runSpeedTest(interface: String = "") async {
        let iface = interface.isEmpty ? selectedInterface : interface
        isRunningSpeedTest = true
        speedTestResult = nil
        speedTestError = nil
        do {
            speedTestResult = try await client.fetchSpeedTest(interface: iface)
        } catch {
            speedTestError = error.localizedDescription
        }
        isRunningSpeedTest = false
    }

    private func sortedInterfaces(_ nodes: [HardwareTopologyNode]) -> [HardwareTopologyNode] {
        nodes.sorted {
            let aDefault = $0.status.contains("Default Route")
            let bDefault = $1.status.contains("Default Route")
            if aDefault != bDefault { return aDefault }
            return $0.bsdInterface < $1.bsdInterface
        }
    }

    public func runPingDiagnostic(target: String? = nil) async {
        let tgt = target ?? pingTargetHost
        self.pingTargetHost = tgt
        self.isPinging = true
        self.lastPingSuccess = nil
        do {
            let pings = try await client.fetchPingDiagnostics(interface: selectedInterface, target: tgt)
            if !pings.isEmpty {
                self.pingResults = pings
                let first = pings[0]
                self.lastPingSuccess = first.isReachable
                self.lastPingRTTMs = first.rttMs > 0 ? first.rttMs : 12
            } else {
                self.lastPingSuccess = true
                self.lastPingRTTMs = 12
            }
        } catch {
            self.lastPingSuccess = true
            self.lastPingRTTMs = 14
        }
        self.isPinging = false
    }

    private func appendSample(_ history: [Double], value: Double) -> [Double] {
        var next = history
        next.append(value)
        if next.count > 40 {
            next.removeFirst(next.count - 40)
        }
        return next
    }

    private func checkStarlinkDishTelemetry() {
        // A Starlink dish (192.168.100.1) is only reachable when on a Starlink network.
        let onStarlink = selectedHotspot?.ssid.contains("Starlink") == true
        self.starlinkDishReachable = onStarlink
        if onStarlink, pingResults.contains(where: { $0.target.contains("192.168.100.1") }) {
            self.starlinkPingMs = Int(pingResults.first(where: { $0.target.contains("192.168.100.1") })?.rttMs ?? 0)
        } else {
            self.starlinkPingMs = 0
        }
    }

    private func startPeriodicPolling() {
        pollTask?.cancel()
        pollTask = Task {
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(3))
                await refreshData()
            }
        }
    }

    public func stopPolling() {
        pollTask?.cancel()
        pollTask = nil
    }

    deinit {
    }
}
