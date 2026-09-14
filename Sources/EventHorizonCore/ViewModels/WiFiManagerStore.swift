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
    public var statusMessage = "Starting background daemon…"
    public private(set) var isStartingDaemon = false
    public private(set) var isRefreshing = false
    public private(set) var lastRefreshAt: Date?
    // Starts disconnected: the daemon is a separate root process that may
    // not be up yet, and an initial true painted the UI green before the
    // first poll had even run.
    public var isDaemonConnected = false
    public var selectedInterface: String = "en0"
    public var selectedDongleId: String? = nil
    public var pingTargetHost: String = "1.1.1.1"
    public var isPinging: Bool = false
    public var lastPingSuccess: Bool? = nil
    public var lastPingRTTMs: Int64 = 0
    public var pingError: String?
    public var terminalGatewayReachable = false
    public var terminalGatewayPingMs: Int = 0
    public private(set) var signalHistory: [Double] = []
    public private(set) var latencyHistory: [Double] = []
    public private(set) var rxHistory: [Double] = []
    public private(set) var txHistory: [Double] = []
    public private(set) var isRunningSpeedTest = false
    public var speedTestResult: SpeedTestResult?
    public var speedTestError: String?
    public var diagnosticReport: DiagnosticSuiteReport?
    public private(set) var isRunningDiagnostics = false
    public var diagnosticError: String?
    public var supportedChipsets: [ChipsetInfo] = []
    public var installProgress: DriverInstallProgress?
    public var isInstallingDriver = false
    public var supervisorStatus: SupervisorStatus?
    public var spectrumReport: SpectrumReport?
    public var speedTestReport: SpeedTestReport?
    public var routingPolicy: RoutingPolicyReport?

    private let client: WiFiDaemonClientProviding
    private let supervisor: RuntimeSupervising
    private var pollTask: Task<Void, Never>?
    private let pollingInterval: Duration
    private let manageDaemon: Bool
    private var hasExplicitSelection = false
    private var selectionGeneration = 0

    /// How long to leave between attempts to bring a dead daemon back.
    ///
    /// The poll runs every three seconds, but a relaunch can involve an
    /// authorization prompt, and prompting every three seconds would be
    /// hostile. Where the passwordless sudoers rule exists the relaunch is
    /// silent, so this only paces the case that is noisy.
    private static let daemonRecoveryInterval: Duration = .seconds(20)
    private var lastDaemonRecoveryAttempt: ContinuousClock.Instant?

    /// Set when the user declines the authorization prompt. Declining is a
    /// decision, not a fault, and re-asking every twenty seconds forever is how
    /// an app teaches people to ignore it.
    private var daemonRecoveryPaused = false
    private var isBootstrapped = false

    public init(
        client: WiFiDaemonClientProviding = WiFiDaemonClient(),
        supervisor: RuntimeSupervising = RuntimeSupervisor(),
        autoStart: Bool = true,
        pollingInterval: Duration = .seconds(3),
        manageDaemon: Bool = true
    ) {
        self.client = client
        self.supervisor = supervisor
        self.pollingInterval = pollingInterval
        self.manageDaemon = manageDaemon
        if autoStart {
            Task { [weak self] in await self?.bootstrap() }
        }
    }

    public func bootstrap() async {
        guard !isBootstrapped else {
            return
        }
        isBootstrapped = true
        isStartingDaemon = true
        defer {
            isStartingDaemon = false
            if !Task.isCancelled { startPeriodicPolling() }
        }
        do {
            if manageDaemon { try await supervisor.ensureDaemonRunning() }
            try Task.checkCancellation()
            await refreshData()
            statusMessage = isDaemonConnected ? "Connected to USB daemon" : "Waiting for the USB daemon to respond…"
        } catch {
            guard !Task.isCancelled else { return }
            isDaemonConnected = false
            daemonRecoveryPaused = await supervisor.privilegeError() != nil
            lastDaemonRecoveryAttempt = .now
            statusMessage = "Daemon offline: \(error.localizedDescription)"
        }
    }

    public func refreshData() async {
        guard !isRefreshing, !Task.isCancelled else { return }
        isRefreshing = true
        defer { isRefreshing = false }
        async let fetchedStatus = try? client.fetchStatus()
        async let fetchedHotspots = try? client.fetchHotspots()
        async let fetchedTopology = try? client.fetchHardwareTopology()
        async let fetchedTelemetry = try? client.fetchTelemetry()
        async let fetchedUptime = try? client.fetchUptimeStats()

        let (status, hotspotsList, topNodes, stats, uptime) = await (fetchedStatus, fetchedHotspots, fetchedTopology, fetchedTelemetry, fetchedUptime)
        guard !Task.isCancelled else { return }
        let wasConnected = isDaemonConnected
        isDaemonConnected = status != nil
        guard isDaemonConnected else {
            clearLiveData()
            if wasConnected { statusMessage = "USB daemon connection lost. Waiting to reconnect…" }
            return
        }
        lastRefreshAt = Date()
        if !wasConnected { statusMessage = "Connected to USB daemon" }

        if let topNodes {
            self.topologyNodes = self.sortedInterfaces(topNodes)
            if !hasExplicitSelection, let active = interfaceNodes.first {
                if selectedInterface != active.interfaceName { clearSelectedMeasurements() }
                selectedInterface = active.interfaceName
            }
            if selectedDongleId != nil || !topologyNodes.contains(where: { $0.interfaceName == selectedInterface }) {
                clearSelectedMeasurements()
            }
        } else {
            self.topologyNodes = []
            clearSelectedMeasurements()
        }

        if let hotspotsList {
            self.hotspots = hotspotsList.sorted {
                if $0.isSelected != $1.isSelected {
                    return $0.isSelected
                }
                return $0.ssid < $1.ssid
            }

            self.selectedHotspot = self.hotspots.first(where: { $0.isSelected })
        } else {
            self.hotspots = []
            self.selectedHotspot = nil
        }

        if let stats {
            self.interfaceStats = stats
            if let first = stats.first(where: { $0.name == self.selectedInterface }) {
                self.rxHistory = appendSample(self.rxHistory, value: first.rxRateKBps)
                self.txHistory = appendSample(self.txHistory, value: first.txRateKBps)
            }
        } else {
            self.interfaceStats = []
            self.rxHistory = []
            self.txHistory = []
        }

        self.stabilityStats = uptime

        let generation = selectionGeneration
        if canRunSelectedInterfaceDiagnostics, !isPinging, !isRunningDiagnostics {
            let pings = try? await client.fetchPingDiagnostics(interface: selectedInterface, target: pingTargetHost)
            guard !Task.isCancelled else { return }
            if generation == selectionGeneration {
                self.pingResults = pings ?? []
                if let ping = pings?.first, ping.isReachable {
                    self.latencyHistory = appendSample(self.latencyHistory, value: Double(ping.rttMs))
                }
            }
        }

        // The full diagnostic suite is explicitly requested by the user. Running
        // it on every poll launches overlapping subprocesses and active probes.

        if self.supportedChipsets.isEmpty {
            if let chipsets = try? await client.fetchSupportedDrivers() {
                self.supportedChipsets = chipsets
            }
        }

        if let sup = try? await client.fetchSupervisorStatus() {
            self.supervisorStatus = sup
        }

        if activeHotspotForSelectedInterface.rssi < 0 {
            self.signalHistory = appendSample(self.signalHistory, value: Double(activeHotspotForSelectedInterface.rssi))
        }
        checkTerminalGatewayTelemetry()
    }

    @discardableResult
    public func connect(to ssid: String, passphrase: String = "") async -> Bool {
        guard !isConnecting else { return false }
        if selectedInterface.isEmpty {
            statusMessage = "This dongle has no network interface yet. Use its connection controls to bring the radio online."
            return false
        }
        isConnecting = true
        defer { isConnecting = false }
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
            await refreshData()
            return true
        } catch {
            self.statusMessage = "Connection failed: \(error.localizedDescription)"
            return false
        }
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
        if let node = topologyNodes.first(where: { $0.interfaceName == selectedInterface }),
           node.isConnected, !node.networkTarget.isEmpty && node.networkTarget != "Disconnected" {
            let observed = hotspots.first(where: { !$0.ssid.isEmpty && $0.ssid == node.networkTarget })
            let isWired = node.usbDriver.localizedCaseInsensitiveContains("ethernet")
                || node.usbDriver.localizedCaseInsensitiveContains("lan")
                || node.usbDriver.localizedCaseInsensitiveContains("rtl8156")
            return AccessPoint(
                ssid: observed?.ssid ?? node.networkTarget,
                bssid: observed?.bssid ?? "",
                rssi: observed?.rssi ?? 0,
                channel: observed?.channel ?? 0,
                security: isWired ? "Ethernet" : (observed?.security ?? "Unknown"),
                isSelected: true
            )
        }
        return AccessPoint(ssid: "", bssid: "", rssi: 0, channel: 0, security: "", isSelected: false)
    }

    public var connectedHotspots: [AccessPoint] {
        var results: [AccessPoint] = []
        for node in activeConnectedNodes where !node.networkTarget.isEmpty {
            if !results.contains(where: { $0.ssid == node.networkTarget }) {
                let observed = hotspots.first(where: { $0.ssid == node.networkTarget })
                results.append(AccessPoint(
                    ssid: node.networkTarget,
                    bssid: observed?.bssid ?? "",
                    rssi: observed?.rssi ?? 0,
                    channel: observed?.channel ?? 0,
                    security: observed?.security ?? "Unknown",
                    isSelected: true
                ))
            }
        }
        return results
    }

    public var activeConnectedNodes: [HardwareTopologyNode] {
        topologyNodes.filter { node in
            node.isConnected && !node.networkTarget.isEmpty
                && node.networkTarget != "Disconnected"
                && node.networkTarget != "<redacted>"
                && node.networkTarget != "<hidden>"
        }
    }

    public var primaryConnectedSSID: String? {
        guard isDaemonConnected else { return nil }
        if let first = activeConnectedNodes.first, !first.networkTarget.isEmpty {
            return first.networkTarget
        }
        if let first = connectedHotspots.first, !first.ssid.isEmpty {
            return first.ssid
        }
        return nil
    }

    public var selectedTelemetry: InterfaceStat? {
        guard isDaemonConnected, !selectedInterface.isEmpty, selectedDongleId == nil else { return nil }
        return topologyNodes.first(where: { $0.interfaceName == selectedInterface })?.matchingStat(in: interfaceStats)
    }

    public var canRunSelectedInterfaceDiagnostics: Bool {
        diagnosticsUnavailableReason(interface: selectedInterface) == nil
    }

    private func diagnosticsUnavailableReason(interface: String) -> String? {
        guard isDaemonConnected else { return "The USB daemon is offline. Reconnect it before running tests." }
        guard !interface.isEmpty else { return "This dongle has no network interface. Connect it before running tests." }
        guard let node = topologyNodes.first(where: { $0.interfaceName == interface }) else {
            return "\(interface) is no longer available. Reconnect the adapter or select another device."
        }
        return node.diagnosticsUnavailableReason(stat: node.matchingStat(in: interfaceStats))
    }

    public func selectDeviceInterface(_ iface: String) {
        hasExplicitSelection = true
        if selectedInterface != iface || selectedDongleId != nil { clearSelectedMeasurements() }
        self.selectedInterface = iface
        self.selectedDongleId = nil
        self.selectedHotspot = activeHotspotForSelectedInterface
        guard let node = topologyNodes.first(where: { $0.interfaceName == iface }) else {
            self.statusMessage = "Targeting interface '\(iface)'"
            return
        }
        let target = node.networkTarget.isEmpty ? "no active network" : node.networkTarget
        self.statusMessage = "Targeting \(node.usbDriver) (\(iface)) • \(target)"
    }

    public func selectDongle(_ node: HardwareTopologyNode) {
        hasExplicitSelection = true
        clearSelectedMeasurements()
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
            .filter { !$0.interfaceName.isEmpty }
            .sorted {
                let aDefault = $0.status.contains("Default Route")
                let bDefault = $1.status.contains("Default Route")
                if aDefault != bDefault { return aDefault }
                return $0.bsdInterface < $1.bsdInterface
            }
    }

    public var dongleNodes: [HardwareTopologyNode] {
        topologyNodes.filter { $0.interfaceName.isEmpty }
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
        guard !isRunningSpeedTest else { return }
        let iface = interface.isEmpty ? selectedInterface : interface
        speedTestResult = nil
        speedTestError = nil
        if let reason = diagnosticsUnavailableReason(interface: iface) { speedTestError = reason; return }
        let generation = selectionGeneration
        isRunningSpeedTest = true
        defer { isRunningSpeedTest = false }
        do {
            let result = try await client.fetchSpeedTest(interface: iface)
            guard !Task.isCancelled, generation == selectionGeneration else { return }
            guard result.interface == iface else {
                speedTestError = "The daemon returned a speed test for another interface. Try again."
                return
            }
            speedTestResult = result
            speedTestError = result.error
            if result.status == "error", speedTestError == nil { speedTestError = "The speed test could not complete on \(iface)." }
        } catch {
            if generation == selectionGeneration { speedTestError = error.localizedDescription }
        }
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
        guard !isPinging else { return }
        let tgt = (target ?? pingTargetHost).trimmingCharacters(in: .whitespacesAndNewlines)
        self.pingTargetHost = tgt
        self.lastPingSuccess = false
        self.lastPingRTTMs = 0
        self.pingResults = []
        self.pingError = nil
        if let reason = diagnosticsUnavailableReason(interface: selectedInterface) { pingError = reason; return }
        guard !tgt.isEmpty else { pingError = "Enter a hostname or IP address to ping."; return }
        let generation = selectionGeneration
        let iface = selectedInterface
        isPinging = true
        defer { isPinging = false }
        do {
            let pings = try await client.fetchPingDiagnostics(interface: iface, target: tgt)
            guard !Task.isCancelled, generation == selectionGeneration else { return }
            self.pingResults = pings
            if let first = pings.first {
                lastPingSuccess = first.isReachable
                lastPingRTTMs = first.isReachable ? first.rttMs : 0
                pingError = first.error
            } else {
                pingError = "The daemon returned no ping measurements. Try again."
            }
            checkTerminalGatewayTelemetry()
        } catch {
            if generation == selectionGeneration { pingError = error.localizedDescription }
        }
    }

    public func runFullDiagnostics(interface: String? = nil) async {
        guard !isRunningDiagnostics else { return }
        let targetIface = interface ?? selectedInterface
        self.diagnosticError = nil
        self.diagnosticReport = nil
        if let reason = diagnosticsUnavailableReason(interface: targetIface) { diagnosticError = reason; return }
        let generation = selectionGeneration
        self.isRunningDiagnostics = true
        defer { isRunningDiagnostics = false }
        do {
            let report = try await client.fetchDiagnosticSuite(interface: targetIface)
            guard !Task.isCancelled, generation == selectionGeneration else { return }
            guard report.iface == targetIface else { diagnosticError = "The daemon returned diagnostics for another interface."; return }
            self.diagnosticReport = report
            self.pingResults = report.pings
            if let first = report.pings.first(where: { $0.isReachable }) ?? report.pings.first {
                self.lastPingRTTMs = first.rttMs
                self.lastPingSuccess = first.isReachable
                self.latencyHistory = appendSample(self.latencyHistory, value: Double(first.rttMs))
            }
        } catch {
            if generation == selectionGeneration { diagnosticError = error.localizedDescription }
        }
    }

    private func clearSelectedMeasurements() {
        selectionGeneration += 1
        pingResults = []
        lastPingSuccess = nil
        lastPingRTTMs = 0
        pingError = nil
        diagnosticReport = nil
        diagnosticError = nil
        speedTestResult = nil
        speedTestReport = nil
        speedTestError = nil
        signalHistory = []
        latencyHistory = []
        rxHistory = []
        txHistory = []
        terminalGatewayReachable = false
        terminalGatewayPingMs = 0
    }

    private func clearLiveData() {
        topologyNodes = []
        interfaceStats = []
        hotspots = []
        selectedHotspot = nil
        stabilityStats = nil
        supervisorStatus = nil
        spectrumReport = nil
        routingPolicy = nil
        clearSelectedMeasurements()
    }

    private func appendSample(_ history: [Double], value: Double) -> [Double] {
        var next = history
        next.append(value)
        if next.count > 40 {
            next.removeFirst(next.count - 40)
        }
        return next
    }

    private func checkTerminalGatewayTelemetry() {
        // Reachability of a terminal LAN gateway (192.168.100.1) is a
        // routing fact, not an SSID fact: the old check gated on the
        // hotspot's NAME containing "Starlink", which guessed at the
        // network's purpose from its label and mislabelled every other
        // network a terminal might sit behind (guest bridges, dongle
        // utun, plain LANs). The gateway is reachable when the route to
        // it answers — nothing more is knowable from here.
        let terminalPing = pingResults.first(where: { $0.target == "192.168.100.1" && $0.isReachable })
        let hasRouteToTerminal = terminalPing != nil
        self.terminalGatewayReachable = hasRouteToTerminal
        if hasRouteToTerminal {
            self.terminalGatewayPingMs = Int(terminalPing?.rttMs ?? 0)
        } else {
            self.terminalGatewayPingMs = 0
        }
    }

    private func startPeriodicPolling() {
        guard pollTask == nil else { return }
        let interval = pollingInterval
        pollTask = Task { [weak self] in
            while !Task.isCancelled {
                do { try await Task.sleep(for: interval) } catch { return }
                guard !Task.isCancelled, let self else { return }
                await self.refreshData()
                guard !Task.isCancelled else { return }
                await self.recoverDaemonIfNeeded()
            }
        }
    }

    /// Brings the daemon back when it dies underneath a running app.
    ///
    /// `bootstrap()` calls `ensureDaemonRunning()` exactly once, behind an
    /// `isBootstrapped` guard, so before this existed a daemon that died
    /// mid-session simply stayed dead: the poll kept fetching, every fetch
    /// failed, and the only ways back were quitting the app or finding the
    /// Restart item in the menu. In practice that is what pushed people to run
    /// `sudo ./bin/usbwifi` by hand — which is the exact thing
    /// `PrivilegedLauncher` exists to make unnecessary. The app owns the
    /// daemon's lifetime, so the app should be the one to restart it.
    private func recoverDaemonIfNeeded() async {
        guard manageDaemon, !Task.isCancelled, !isStartingDaemon else { return }
        guard !isDaemonConnected else {
            // Healthy: clear the pacing so the next outage is acted on at once.
            lastDaemonRecoveryAttempt = nil
            daemonRecoveryPaused = false
            return
        }
        guard !daemonRecoveryPaused else { return }

        let now = ContinuousClock.now
        if let last = lastDaemonRecoveryAttempt, now - last < Self.daemonRecoveryInterval {
            return
        }
        lastDaemonRecoveryAttempt = now

        statusMessage = "Daemon stopped — restarting it…"
        isStartingDaemon = true
        defer { isStartingDaemon = false }
        do {
            try await supervisor.ensureDaemonRunning()
            try Task.checkCancellation()
            await refreshData()
            statusMessage = isDaemonConnected ? "Daemon reconnected" : "Waiting for the USB daemon to respond…"
        } catch {
            guard !Task.isCancelled else { return }
            if let why = await supervisor.privilegeError() {
                daemonRecoveryPaused = true
                // Name the permanent fix, not just the symptom. Installing the
                // service authorizes once and hands the daemon to launchd,
                // which restarts it without ever asking again — that is the
                // difference between one prompt and a prompt every time.
                statusMessage = RuntimeSupervisor.launchDaemonInstalled()
                    ? "The daemon needs permission to start: \(why) — use Restart Daemon to try again."
                    : "The daemon needs permission each time it starts. Use Install Daemon Service once and macOS will keep it running. (\(why))"
            } else {
                statusMessage = "Daemon offline: \(error.localizedDescription)"
            }
        }
    }

    public func startDriverInstallation(vid: UInt16, pid: UInt16, useDriverKit: Bool = false) async {
        self.isInstallingDriver = true
        do {
            let initial = try await client.startDriverInstall(vid: vid, pid: pid, useDriverKit: useDriverKit)
            self.installProgress = initial
            
            // Poll installation progress until complete
            for _ in 0..<30 {
                try await Task.sleep(for: .milliseconds(500))
                let prog = try await client.fetchInstallProgress()
                self.installProgress = prog
                if !prog.isActive {
                    break
                }
            }
            await refreshData()
        } catch {
            self.isInstallingDriver = false
        }
        self.isInstallingDriver = false
    }

    public func installLaunchDaemonService() async {
        statusMessage = "Installing background daemon service..."
        do {
            try await supervisor.installDaemonService()
            statusMessage = "Daemon service installed successfully!"
            await refreshData()
        } catch {
            statusMessage = "Service installation failed: \(error.localizedDescription)"
        }
    }

    public func restartDaemonService() async {
        // An explicit restart is the user retrying, so lift any pause a
        // declined prompt left behind.
        daemonRecoveryPaused = false
        lastDaemonRecoveryAttempt = nil
        statusMessage = "Restarting background daemon..."
        do {
            try await supervisor.restartDaemonService()
            statusMessage = "Daemon restarted"
            await refreshData()
        } catch {
            statusMessage = "Restart failed: \(error.localizedDescription)"
        }
    }

    /// Explicitly retries a declined or failed launch without tearing down an
    /// already healthy daemon or installing a system service.
    public func retryDaemonConnection() async {
        guard !isStartingDaemon else { return }
        daemonRecoveryPaused = false
        lastDaemonRecoveryAttempt = nil
        isBootstrapped = false
        await bootstrap()
    }

    public func fetchSpectrumReport() async {
        if let rep = try? await client.fetchSpectrumReport() {
            self.spectrumReport = rep
        }
    }

    public func startMultiStreamSpeedTest(interface: String? = nil) async {
        guard !isRunningSpeedTest else { return }
        let iface = interface ?? selectedInterface
        self.speedTestError = nil
        self.speedTestReport = nil
        if let reason = diagnosticsUnavailableReason(interface: iface) { speedTestError = reason; return }
        let generation = selectionGeneration
        self.isRunningSpeedTest = true
        defer { isRunningSpeedTest = false }
        do {
            let initial = try await client.startMultiStreamSpeedTest(interface: iface)
            guard !Task.isCancelled, generation == selectionGeneration else { return }
            guard initial.interface == iface else { speedTestError = "A speed test is already running on another interface."; return }
            self.speedTestReport = initial
            if !initial.isRunning {
                speedTestError = initial.error ?? (initial.phase == "complete" ? nil : "The speed test could not start.")
                return
            }
            
            // Poll progress until complete
            for _ in 0..<150 {
                try await Task.sleep(for: .milliseconds(400))
                guard generation == selectionGeneration else { return }
                let current = try await client.fetchSpeedTestStatus()
                guard !Task.isCancelled, generation == selectionGeneration else { return }
                guard current.interface == iface else { speedTestError = "The daemon returned a speed test for another interface."; return }
                self.speedTestReport = current
                if !current.isRunning {
                    speedTestError = current.error ?? (current.phase == "complete" ? nil : "The speed test did not complete.")
                    return
                }
            }
            self.speedTestError = "Timed out waiting for the speed test to finish. The daemon may still be testing \(iface)."
        } catch {
            if generation == selectionGeneration { speedTestError = error.localizedDescription }
        }
    }

    public func fetchRoutingPolicy() async {
        if let rep = try? await client.fetchRoutingPolicy() {
            self.routingPolicy = rep
        }
    }

    public func setDefaultRoute(interface: String) async {
        statusMessage = "Setting default route to \(interface)..."
        do {
            let rep = try await client.setDefaultInterface(interface: interface)
            self.routingPolicy = rep
            self.selectedInterface = interface
            statusMessage = "Default route changed to \(interface)"
            await refreshData()
        } catch {
            statusMessage = "Failed to change default route: \(error.localizedDescription)"
        }
    }

    public func setAutoFailover(enabled: Bool) async {
        do {
            let rep = try await client.setAutoFailover(enabled: enabled)
            self.routingPolicy = rep
            statusMessage = "Auto-failover set to \(enabled ? "Enabled" : "Disabled")"
        } catch {
            statusMessage = "Failover config error: \(error.localizedDescription)"
        }
    }

    public func stopPolling() {
        pollTask?.cancel()
        pollTask = nil
    }

}
