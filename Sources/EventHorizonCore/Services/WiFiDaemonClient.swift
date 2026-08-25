import Foundation

public protocol WiFiDaemonClientProviding: Sendable {
    func fetchHotspots() async throws -> [AccessPoint]
    func connectToHotspot(ssid: String, passphrase: String) async throws -> AccessPoint
    func disconnectFromNetwork() async throws
    func fetchStatus() async throws -> DaemonStatus
    func fetchTelemetry() async throws -> [InterfaceStat]
    func fetchHardwareTopology() async throws -> [HardwareTopologyNode]
    func switchDongleToWiFiMode() async throws
    func fetchPingDiagnostics(interface: String, target: String) async throws -> [PingResult]
    func fetchUptimeStats() async throws -> StabilityStats
    func fetchSpeedTest(interface: String) async throws -> SpeedTestResult
    func fetchDiagnosticSuite(interface: String?) async throws -> DiagnosticSuiteReport
    func fetchSupportedDrivers() async throws -> [ChipsetInfo]
    func startDriverInstall(vid: UInt16, pid: UInt16, useDriverKit: Bool) async throws -> DriverInstallProgress
    func fetchInstallProgress() async throws -> DriverInstallProgress
    func fetchSupervisorStatus() async throws -> SupervisorStatus
    func fetchSpectrumReport() async throws -> SpectrumReport
    func startMultiStreamSpeedTest(interface: String) async throws -> SpeedTestReport
    func fetchSpeedTestStatus() async throws -> SpeedTestReport
    func fetchRoutingPolicy() async throws -> RoutingPolicyReport
    func setDefaultInterface(interface: String) async throws -> RoutingPolicyReport
    func setAutoFailover(enabled: Bool) async throws -> RoutingPolicyReport
}

public actor WiFiDaemonClient: WiFiDaemonClientProviding {
    private let baseURL: URL
    private let session: URLSession

    public init(baseURL: URL = URL(string: "http://127.0.0.1:8990")!) {
        self.baseURL = baseURL
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 5.0
        self.session = URLSession(configuration: config)
    }

    public func fetchPingDiagnostics(interface: String = "en0", target: String = "1.1.1.1") async throws -> [PingResult] {
        var components = URLComponents(url: baseURL.appendingPathComponent("api/diagnostics/ping"), resolvingAgainstBaseURL: false)!
        components.queryItems = [
            URLQueryItem(name: "interface", value: interface),
            URLQueryItem(name: "target", value: target)
        ]
        let url = components.url ?? baseURL.appendingPathComponent("api/diagnostics/ping")
        let (data, response) = try await session.data(from: url)

        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }

        let decoded = try JSONDecoder().decode(APIResponse<[PingResult]>.self, from: data)
        return decoded.data ?? []
    }

    public func fetchUptimeStats() async throws -> StabilityStats {
        let url = baseURL.appendingPathComponent("api/diagnostics/uptime")
        let (data, response) = try await session.data(from: url)

        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }

        let decoded = try JSONDecoder().decode(APIResponse<StabilityStats>.self, from: data)
        guard let stats = decoded.data else {
            throw URLError(.cannotParseResponse)
        }
        return stats
    }

    public func fetchSpeedTest(interface: String = "en0") async throws -> SpeedTestResult {
        var components = URLComponents(url: baseURL.appendingPathComponent("api/diagnostics/speedtest"), resolvingAgainstBaseURL: false)!
        components.queryItems = [URLQueryItem(name: "interface", value: interface)]
        let url = components.url ?? baseURL.appendingPathComponent("api/diagnostics/speedtest")
        let (data, response) = try await session.data(from: url)

        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }

        let decoded = try JSONDecoder().decode(APIResponse<SpeedTestResult>.self, from: data)
        guard let result = decoded.data else {
            throw URLError(.cannotParseResponse)
        }
        return result
    }

    public func fetchTelemetry() async throws -> [InterfaceStat] {
        let url = baseURL.appendingPathComponent("api/network/telemetry")
        let (data, response) = try await session.data(from: url)

        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }

        let decoded = try JSONDecoder().decode(APIResponse<[InterfaceStat]>.self, from: data)
        return decoded.data ?? []
    }

    public func fetchHardwareTopology() async throws -> [HardwareTopologyNode] {
        let url = baseURL.appendingPathComponent("api/hardware/topology")
        let (data, response) = try await session.data(from: url)

        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }

        let decoded = try JSONDecoder().decode(APIResponse<[HardwareTopologyNode]>.self, from: data)
        return decoded.data ?? []
    }

    public func switchDongleToWiFiMode() async throws {
        let url = baseURL.appendingPathComponent("api/usb/modeswitch")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"

        let (_, response) = try await session.data(for: request)
        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }
    }

    public func fetchHotspots() async throws -> [AccessPoint] {
        let url = baseURL.appendingPathComponent("api/wifi/scan")
        let (data, response) = try await session.data(from: url)

        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }

        let decoded = try JSONDecoder().decode(APIResponse<[AccessPoint]>.self, from: data)
        return decoded.data ?? []
    }

    public func connectToHotspot(ssid: String, passphrase: String) async throws -> AccessPoint {
        let url = baseURL.appendingPathComponent("api/wifi/connect")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        let payload: [String: String] = ["ssid": ssid, "passphrase": passphrase]
        request.httpBody = try JSONEncoder().encode(payload)

        let (data, response) = try await session.data(for: request)

        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }

        let decoded = try JSONDecoder().decode(APIResponse<AccessPoint>.self, from: data)
        guard let ap = decoded.data else {
            throw URLError(.cannotParseResponse)
        }
        return ap
    }

    public func disconnectFromNetwork() async throws {
        let url = baseURL.appendingPathComponent("api/wifi/disconnect")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"

        let (_, response) = try await session.data(for: request)
        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }
    }

    public func fetchStatus() async throws -> DaemonStatus {
        let url = baseURL.appendingPathComponent("api/status")
        let (data, response) = try await session.data(from: url)

        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }

        let decoded = try JSONDecoder().decode(APIResponse<DaemonStatus>.self, from: data)
        guard let status = decoded.data else {
            throw URLError(.cannotParseResponse)
        }
        return status
    }

    public func fetchDiagnosticSuite(interface: String? = nil) async throws -> DiagnosticSuiteReport {
        var components = URLComponents(url: baseURL.appendingPathComponent("api/diagnostics/suite"), resolvingAgainstBaseURL: false)
        if let iface = interface, !iface.isEmpty {
            components?.queryItems = [URLQueryItem(name: "interface", value: iface)]
        }
        guard let url = components?.url else {
            throw URLError(.badURL)
        }

        let (data, response) = try await session.data(from: url)
        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }

        let decoded = try JSONDecoder().decode(APIResponse<DiagnosticSuiteReport>.self, from: data)
        guard let report = decoded.data else {
            throw URLError(.cannotParseResponse)
        }
        return report
    }

    public func fetchSupportedDrivers() async throws -> [ChipsetInfo] {
        let url = baseURL.appendingPathComponent("api/drivers/supported")
        let (data, response) = try await session.data(from: url)

        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }

        let decoded = try JSONDecoder().decode(APIResponse<[ChipsetInfo]>.self, from: data)
        return decoded.data ?? []
    }

    public func startDriverInstall(vid: UInt16, pid: UInt16, useDriverKit: Bool) async throws -> DriverInstallProgress {
        let url = baseURL.appendingPathComponent("api/driver/install")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        let payload: [String: Any] = [
            "vid": vid,
            "pid": pid,
            "use_driverkit": useDriverKit,
            "force_reinstall": true
        ]
        request.httpBody = try JSONSerialization.data(withJSONObject: payload)

        let (data, response) = try await session.data(for: request)
        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }

        let decoded = try JSONDecoder().decode(APIResponse<DriverInstallProgress>.self, from: data)
        guard let progress = decoded.data else {
            throw URLError(.cannotParseResponse)
        }
        return progress
    }

    public func fetchInstallProgress() async throws -> DriverInstallProgress {
        let url = baseURL.appendingPathComponent("api/driver/install/progress")
        let (data, response) = try await session.data(from: url)

        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }

        let decoded = try JSONDecoder().decode(APIResponse<DriverInstallProgress>.self, from: data)
        guard let progress = decoded.data else {
            throw URLError(.cannotParseResponse)
        }
        return progress
    }

    public func fetchSupervisorStatus() async throws -> SupervisorStatus {
        let url = baseURL.appendingPathComponent("api/supervisor/status")
        let (data, response) = try await session.data(from: url)

        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }

        let decoded = try JSONDecoder().decode(APIResponse<SupervisorStatus>.self, from: data)
        guard let status = decoded.data else {
            throw URLError(.cannotParseResponse)
        }
        return status
    }

    public func fetchSpectrumReport() async throws -> SpectrumReport {
        let url = baseURL.appendingPathComponent("api/wifi/spectrum")
        let (data, response) = try await session.data(from: url)
        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }
        let decoded = try JSONDecoder().decode(APIResponse<SpectrumReport>.self, from: data)
        guard let report = decoded.data else {
            throw URLError(.cannotParseResponse)
        }
        return report
    }

    public func startMultiStreamSpeedTest(interface: String = "en0") async throws -> SpeedTestReport {
        var components = URLComponents(url: baseURL.appendingPathComponent("api/diagnostics/speedtest/start"), resolvingAgainstBaseURL: false)!
        components.queryItems = [URLQueryItem(name: "interface", value: interface)]
        let url = components.url ?? baseURL.appendingPathComponent("api/diagnostics/speedtest/start")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"

        let (data, response) = try await session.data(for: request)
        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }
        let decoded = try JSONDecoder().decode(APIResponse<SpeedTestReport>.self, from: data)
        guard let report = decoded.data else {
            throw URLError(.cannotParseResponse)
        }
        return report
    }

    public func fetchSpeedTestStatus() async throws -> SpeedTestReport {
        let url = baseURL.appendingPathComponent("api/diagnostics/speedtest/status")
        let (data, response) = try await session.data(from: url)
        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }
        let decoded = try JSONDecoder().decode(APIResponse<SpeedTestReport>.self, from: data)
        guard let report = decoded.data else {
            throw URLError(.cannotParseResponse)
        }
        return report
    }

    public func fetchRoutingPolicy() async throws -> RoutingPolicyReport {
        let url = baseURL.appendingPathComponent("api/routing/policy")
        let (data, response) = try await session.data(from: url)
        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }
        let decoded = try JSONDecoder().decode(APIResponse<RoutingPolicyReport>.self, from: data)
        guard let report = decoded.data else {
            throw URLError(.cannotParseResponse)
        }
        return report
    }

    public func setDefaultInterface(interface: String) async throws -> RoutingPolicyReport {
        let url = baseURL.appendingPathComponent("api/routing/set-default")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let payload = ["interface": interface]
        request.httpBody = try JSONSerialization.data(withJSONObject: payload)

        let (data, response) = try await session.data(for: request)
        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }
        let decoded = try JSONDecoder().decode(APIResponse<RoutingPolicyReport>.self, from: data)
        guard let report = decoded.data else {
            throw URLError(.cannotParseResponse)
        }
        return report
    }

    public func setAutoFailover(enabled: Bool) async throws -> RoutingPolicyReport {
        let url = baseURL.appendingPathComponent("api/routing/failover")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let payload = ["enabled": enabled]
        request.httpBody = try JSONSerialization.data(withJSONObject: payload)

        let (data, response) = try await session.data(for: request)
        guard let httpResp = response as? HTTPURLResponse, httpResp.statusCode == 200 else {
            throw URLError(.badServerResponse)
        }
        let decoded = try JSONDecoder().decode(APIResponse<RoutingPolicyReport>.self, from: data)
        guard let report = decoded.data else {
            throw URLError(.cannotParseResponse)
        }
        return report
    }
}
