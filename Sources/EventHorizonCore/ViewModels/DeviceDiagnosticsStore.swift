import Foundation
import Observation

public protocol DeviceProbeClient: Sendable {
    func fetchPingDiagnostics(interface: String, target: String) async throws -> [PingResult]
    func fetchSpeedTest(interface: String) async throws -> SpeedTestResult
}

extension WiFiDaemonClient: DeviceProbeClient {}

@Observable @MainActor
public final class DeviceDiagnosticsStore {
    public private(set) var isRunning = false
    public private(set) var pings: [PingResult] = []
    public private(set) var speed: SpeedTestResult?
    public private(set) var error: String?
    private let client: any DeviceProbeClient
    private var generation = 0

    public init(client: any DeviceProbeClient = WiFiDaemonClient()) { self.client = client }

    public func reset() {
        generation += 1
        isRunning = false
        pings = []
        speed = nil
        error = nil
    }

    public func runPing(node: HardwareTopologyNode, stat: InterfaceStat?, target: String) async {
        guard !isRunning else { return }
        reset()
        if let reason = node.diagnosticsUnavailableReason(stat: stat) { error = reason; return }
        let target = target.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !target.isEmpty else { error = "Enter an IP address or hostname to test."; return }
        let requestGeneration = generation
        isRunning = true
        defer { if generation == requestGeneration { isRunning = false } }
        do {
            let results = try await client.fetchPingDiagnostics(interface: node.interfaceName, target: target)
            guard generation == requestGeneration, !Task.isCancelled else { return }
            pings = results
            if results.isEmpty { error = "The daemon returned no ping measurements." }
        } catch {
            guard generation == requestGeneration, !Task.isCancelled else { return }
            self.error = error.localizedDescription
        }
    }

    public func runSpeedTest(node: HardwareTopologyNode, stat: InterfaceStat?) async {
        guard !isRunning else { return }
        reset()
        if let reason = node.diagnosticsUnavailableReason(stat: stat) { error = reason; return }
        let requestGeneration = generation
        isRunning = true
        defer { if generation == requestGeneration { isRunning = false } }
        do {
            let result = try await client.fetchSpeedTest(interface: node.interfaceName)
            guard generation == requestGeneration, !Task.isCancelled else { return }
            guard result.interface == node.interfaceName else {
                error = "The daemon returned a result for a different adapter. Run the test again."
                return
            }
            speed = result
            error = result.error
            if error == nil && result.status != "success" && result.status != "completed" {
                error = "Speed test \(result.status). Some measurements may be unavailable."
            }
        } catch {
            guard generation == requestGeneration, !Task.isCancelled else { return }
            self.error = error.localizedDescription
        }
    }
}
