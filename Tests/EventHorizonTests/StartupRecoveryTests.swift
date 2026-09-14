import XCTest
@testable import EventHorizonCore

final class StartupRecoveryTests: XCTestCase {
    private actor Client: WiFiDaemonClientProviding {
        var healthy = true
        var nodes: [HardwareTopologyNode] = []
        var telemetry: [InterfaceStat] = []
        var pings: [PingResult] = []
        var pingFails = false
        var statusCalls = 0
        var pingCalls = 0
        var diagnosticCalls = 0
        var speedCalls = 0
        func configure(healthy: Bool = true, nodes: [HardwareTopologyNode] = [], telemetry: [InterfaceStat] = [], pings: [PingResult] = [], pingFails: Bool = false) {
            self.healthy = healthy; self.nodes = nodes; self.telemetry = telemetry
            self.pings = pings; self.pingFails = pingFails
        }
        func fetchStatus() async throws -> DaemonStatus {
            statusCalls += 1
            guard healthy else { throw URLError(.cannotConnectToHost) }
            return DaemonStatus(version: "test", hotspots: 0, arch: "arm64", os: "macOS")
        }
        func fetchHardwareTopology() async throws -> [HardwareTopologyNode] { nodes }
        func fetchTelemetry() async throws -> [InterfaceStat] { telemetry }
        func fetchHotspots() async throws -> [AccessPoint] { [] }
        func fetchPingDiagnostics(interface: String, target: String) async throws -> [PingResult] {
            pingCalls += 1
            if pingFails { throw URLError(.timedOut) }
            return pings
        }
        func fetchDiagnosticSuite(interface: String?) async throws -> DiagnosticSuiteReport { diagnosticCalls += 1; throw URLError(.unsupportedURL) }
        func fetchSpeedTest(interface: String) async throws -> SpeedTestResult { speedCalls += 1; throw URLError(.unsupportedURL) }
        func startMultiStreamSpeedTest(interface: String) async throws -> SpeedTestReport { speedCalls += 1; throw URLError(.unsupportedURL) }
        func connectToHotspot(ssid: String, passphrase: String) async throws -> AccessPoint { throw URLError(.unsupportedURL) }
        func disconnectFromNetwork() async throws { throw URLError(.unsupportedURL) }
        func switchDongleToWiFiMode() async throws { throw URLError(.unsupportedURL) }
        func fetchUptimeStats() async throws -> StabilityStats { throw URLError(.unsupportedURL) }
        func fetchSupportedDrivers() async throws -> [ChipsetInfo] { [] }
        func startDriverInstall(vid: UInt16, pid: UInt16, useDriverKit: Bool) async throws -> DriverInstallProgress { throw URLError(.unsupportedURL) }
        func fetchInstallProgress() async throws -> DriverInstallProgress { throw URLError(.unsupportedURL) }
        func fetchSupervisorStatus() async throws -> SupervisorStatus { throw URLError(.unsupportedURL) }
        func fetchSpectrumReport() async throws -> SpectrumReport { throw URLError(.unsupportedURL) }
        func fetchSpeedTestStatus() async throws -> SpeedTestReport { throw URLError(.unsupportedURL) }
        func fetchRoutingPolicy() async throws -> RoutingPolicyReport { throw URLError(.unsupportedURL) }
        func setDefaultInterface(interface: String) async throws -> RoutingPolicyReport { throw URLError(.unsupportedURL) }
        func setAutoFailover(enabled: Bool) async throws -> RoutingPolicyReport { throw URLError(.unsupportedURL) }
    }

    private actor Supervisor: RuntimeSupervising {
        var attempts = 0
        let declined: Bool
        init(declined: Bool = false) { self.declined = declined }
        func ensureDaemonRunning() async throws {
            attempts += 1
            if declined { throw PrivilegedLauncher.LaunchError.cancelled }
            throw URLError(.cannotConnectToHost)
        }
        func privilegeError() async -> String? { declined ? "Authorization was declined" : nil }
        func installDaemonService() async throws {}
        func restartDaemonService() async throws {}
    }

    private func node(_ interface: String, status: String = "Up", target: String = "Wi-Fi") -> HardwareTopologyNode {
        HardwareTopologyNode(usbDriver: "USB Wi-Fi", vendorId: "0x1234", productId: "0x5678", serialNumber: interface,
                             speed: "480 Mbps", bsdInterface: interface, networkTarget: target,
                             ipAddress: "192.0.2.10", subnetMask: "255.255.255.0", gateway: "192.0.2.1",
                             macAddress: "", status: status, driverType: "test")
    }

    @MainActor
    private func store(_ client: Client) async -> WiFiManagerStore {
        let result = WiFiManagerStore(client: client, supervisor: Supervisor())
        try? await Task.sleep(for: .milliseconds(20))
        result.stopPolling()
        return result
    }

    @MainActor
    func testStartupFailureKeepsWatchingForExternallyStartedDaemon() async {
        let client = Client()
        await client.configure(healthy: false)
        let store = WiFiManagerStore(client: client, supervisor: Supervisor(declined: true))
        defer { store.stopPolling() }
        try? await Task.sleep(for: .milliseconds(30))
        await client.configure(healthy: true)
        try? await Task.sleep(for: .milliseconds(3250))
        XCTAssertTrue(store.isDaemonConnected, "Declining authorization must still allow reconnecting when a daemon becomes available")
    }

    @MainActor
    func testSelectedExternalInterfaceSurvivesRefresh() async {
        let client = Client()
        await client.configure(nodes: [node("en0", status: "Up Default Route"), node("utun8")])
        let store = await store(client)
        store.selectDeviceInterface("utun8")
        await store.refreshData()
        XCTAssertEqual(store.selectedInterface, "utun8")
    }

    @MainActor
    func testEmptyTopologyClearsUnpluggedDevices() async {
        let client = Client()
        let store = await store(client)
        store.topologyNodes = [node("utun8")]
        store.selectDeviceInterface("utun8")
        await store.refreshData()
        XCTAssertTrue(store.topologyNodes.isEmpty)
        XCTAssertEqual(store.selectedInterface, "utun8", "Unplugging must not silently retarget diagnostics to another adapter")
    }

    @MainActor
    func testOfflineHeartbeatClearsLiveTelemetry() async {
        let client = Client()
        await client.configure(healthy: false)
        let store = await store(client)
        store.topologyNodes = [node("utun8")]
        store.pingResults = [PingResult(target: "1.1.1.1", isReachable: true, rttMs: 4, packetLossPercent: 0)]
        store.lastPingSuccess = true
        await store.refreshData()
        XCTAssertTrue(store.topologyNodes.isEmpty)
        XCTAssertTrue(store.pingResults.isEmpty)
        XCTAssertNil(store.lastPingSuccess)
    }

    @MainActor
    func testPingFailureNeverFabricatesReachability() async {
        let client = Client()
        await client.configure(pingFails: true)
        let store = await store(client)
        store.isDaemonConnected = true
        store.topologyNodes = [node("en0")]
        await store.runPingDiagnostic()
        XCTAssertEqual(store.lastPingSuccess, false)
        XCTAssertEqual(store.lastPingRTTMs, 0)
    }

    @MainActor
    func testEmptyPingResponseIsFailure() async {
        let client = Client()
        let store = await store(client)
        store.isDaemonConnected = true
        store.topologyNodes = [node("en0")]
        await store.runPingDiagnostic()
        XCTAssertEqual(store.lastPingSuccess, false)
        XCTAssertEqual(store.lastPingRTTMs, 0)
    }

    @MainActor
    func testUnmappedDongleDoesNotTestDefaultNetwork() async {
        let client = Client()
        let store = await store(client)
        store.isDaemonConnected = true
        store.selectDongle(node(""))
        await store.runPingDiagnostic()
        await store.runSpeedTest()
        await store.startMultiStreamSpeedTest()
        await store.runFullDiagnostics()
        let pingCalls = await client.pingCalls
        let speedCalls = await client.speedCalls
        let diagnosticCalls = await client.diagnosticCalls
        XCTAssertEqual(pingCalls, 0)
        XCTAssertEqual(speedCalls, 0)
        XCTAssertEqual(diagnosticCalls, 0)
    }

    @MainActor
    func testSelectionChangeClearsOtherAdaptersPing() async {
        let client = Client()
        let store = await store(client)
        store.pingResults = [PingResult(target: "1.1.1.1", isReachable: true, rttMs: 4, packetLossPercent: 0)]
        store.lastPingSuccess = true
        store.selectDeviceInterface("utun8")
        XCTAssertTrue(store.pingResults.isEmpty)
        XCTAssertNil(store.lastPingSuccess)
    }

    @MainActor
    func testUnreachableTerminalPingDoesNotClaimReachable() async {
        let client = Client()
        await client.configure(nodes: [node("en0")], pings: [PingResult(target: "192.168.100.1", isReachable: false, rttMs: 0, packetLossPercent: 100)])
        let store = await store(client)
        await store.refreshData()
        XCTAssertFalse(store.terminalGatewayReachable)
    }
}
