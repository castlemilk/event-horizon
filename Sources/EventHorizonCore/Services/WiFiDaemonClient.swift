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
}
