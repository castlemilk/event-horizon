import XCTest
@testable import EventHorizonCore

/// The connected flag is the only thing standing between a dead daemon and
/// its automatic relaunch: `recoverDaemonIfNeeded` returns early while the
/// flag is true. This pins the flag's contract — it follows the status fetch
/// of *this* poll, never retained display data.
final class DaemonConnectionTests: XCTestCase {

    struct StubError: Error {}

    struct StubClient: WiFiDaemonClientProviding {
        var status: DaemonStatus?
        var topology: [HardwareTopologyNode] = []

        func fetchStatus() async throws -> DaemonStatus {
            guard let status else { throw StubError() }
            return status
        }
        func fetchHardwareTopology() async throws -> [HardwareTopologyNode] { topology }

        func fetchHotspots() async throws -> [AccessPoint] { [] }
        func connectToHotspot(ssid: String, passphrase: String) async throws -> AccessPoint { throw StubError() }
        func disconnectFromNetwork() async throws { throw StubError() }
        func fetchTelemetry() async throws -> [InterfaceStat] { throw StubError() }
        func switchDongleToWiFiMode() async throws { throw StubError() }
        func fetchPingDiagnostics(interface: String, target: String) async throws -> [PingResult] { throw StubError() }
        func fetchUptimeStats() async throws -> StabilityStats { throw StubError() }
        func fetchSpeedTest(interface: String) async throws -> SpeedTestResult { throw StubError() }
        func fetchDiagnosticSuite(interface: String?) async throws -> DiagnosticSuiteReport { throw StubError() }
        func fetchSupportedDrivers() async throws -> [ChipsetInfo] { throw StubError() }
        func startDriverInstall(vid: UInt16, pid: UInt16, useDriverKit: Bool) async throws -> DriverInstallProgress { throw StubError() }
        func fetchInstallProgress() async throws -> DriverInstallProgress { throw StubError() }
        func fetchSupervisorStatus() async throws -> SupervisorStatus { throw StubError() }
        func fetchSpectrumReport() async throws -> SpectrumReport { throw StubError() }
        func startMultiStreamSpeedTest(interface: String) async throws -> SpeedTestReport { throw StubError() }
        func fetchSpeedTestStatus() async throws -> SpeedTestReport { throw StubError() }
        func fetchRoutingPolicy() async throws -> RoutingPolicyReport { throw StubError() }
        func setDefaultInterface(interface: String) async throws -> RoutingPolicyReport { throw StubError() }
        func setAutoFailover(enabled: Bool) async throws -> RoutingPolicyReport { throw StubError() }
    }

    struct DeadSupervisor: RuntimeSupervising {
        func ensureDaemonRunning() async throws { throw StubError() }
        func installDaemonService() async throws { throw StubError() }
        func restartDaemonService() async throws { throw StubError() }
    }

    private func staleNode() -> HardwareTopologyNode {
        HardwareTopologyNode(
            usbDriver: "en0", vendorId: "", productId: "", serialNumber: "",
            speed: "", bsdInterface: "en0", networkTarget: "Default Route",
            ipAddress: "", subnetMask: "", gateway: "", macAddress: "",
            status: "Default Route", driverType: ""
        )
    }

    @MainActor
    private func makeStore(client: StubClient) async -> WiFiManagerStore {
        let store = WiFiManagerStore(client: client, supervisor: DeadSupervisor())
        // Let init's bootstrap task settle (its supervisor throws
        // immediately, so this is fast and prompt-free by construction).
        try? await Task.sleep(for: .milliseconds(300))
        return store
    }

    /// Retained topology from a healthy past must not keep the flag green
    /// after the daemon dies. This is the exact failure seen live: the daemon
    /// crashed, topology stayed populated, the flag latched true, and the
    /// auto-relaunch never fired.
    @MainActor
    func testFailedStatusClearsFlagDespiteStaleTopology() async {
        let store = await makeStore(client: StubClient(status: nil, topology: [staleNode()]))
        await store.refreshData()
        XCTAssertFalse(
            store.isDaemonConnected,
            "status fetch failed: the flag must clear even with retained topology nodes"
        )
    }

    @MainActor
    func testHealthyStatusSetsFlag() async {
        let status = DaemonStatus(version: "test", hotspots: 0, arch: "arm64", os: "macOS")
        let store = await makeStore(client: StubClient(status: status))
        await store.refreshData()
        XCTAssertTrue(store.isDaemonConnected)
    }
}
