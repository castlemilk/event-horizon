import Foundation

public struct InterfaceStat: Identifiable, Codable, Sendable, Equatable {
    public var id: String { name }
    public let name: String
    public let bytesIn: UInt64
    public let bytesOut: UInt64
    public let packetsIn: UInt64
    public let packetsOut: UInt64
    public let errorsIn: UInt64
    public let errorsOut: UInt64
    public let isUp: Bool
    public let rxRateKBps: Double
    public let txRateKBps: Double

    enum CodingKeys: String, CodingKey {
        case name
        case bytesIn = "bytes_in"
        case bytesOut = "bytes_out"
        case packetsIn = "packets_in"
        case packetsOut = "packets_out"
        case errorsIn = "errors_in"
        case errorsOut = "errors_out"
        case isUp = "is_up"
        case rxRateKBps = "rx_rate_kbps"
        case txRateKBps = "tx_rate_kbps"
    }

    public init(name: String, bytesIn: UInt64, bytesOut: UInt64, packetsIn: UInt64, packetsOut: UInt64, errorsIn: UInt64, errorsOut: UInt64, isUp: Bool, rxRateKBps: Double, txRateKBps: Double) {
        self.name = name
        self.bytesIn = bytesIn
        self.bytesOut = bytesOut
        self.packetsIn = packetsIn
        self.packetsOut = packetsOut
        self.errorsIn = errorsIn
        self.errorsOut = errorsOut
        self.isUp = isUp
        self.rxRateKBps = rxRateKBps
        self.txRateKBps = txRateKBps
    }
}

public struct HardwareTopologyNode: Identifiable, Codable, Sendable, Equatable {
    public var id: String {
        if !bsdInterface.isEmpty {
            return bsdInterface
        }
        if !vendorId.isEmpty || !productId.isEmpty || !serialNumber.isEmpty {
            return "\(vendorId):\(productId):\(serialNumber)"
        }
        return usbDriver
    }
    public let usbDriver: String
    public let vendorId: String
    public let productId: String
    public let serialNumber: String
    public let speed: String
    public let bsdInterface: String
    public let networkTarget: String
    public let ipAddress: String
    public let subnetMask: String
    public let gateway: String
    public let macAddress: String
    public let status: String
    public let driverType: String

    enum CodingKeys: String, CodingKey {
        case usbDriver = "usb_driver"
        case vendorId = "vendor_id"
        case productId = "product_id"
        case serialNumber = "serial_number"
        case speed
        case bsdInterface = "bsd_interface"
        case networkTarget = "network_target"
        case ipAddress = "ip_address"
        case subnetMask = "subnet_mask"
        case gateway
        case macAddress = "mac_address"
        case status
        case driverType = "driver_type"
    }

    public init(usbDriver: String, vendorId: String, productId: String, serialNumber: String, speed: String, bsdInterface: String, networkTarget: String, ipAddress: String, subnetMask: String, gateway: String, macAddress: String, status: String, driverType: String) {
        self.usbDriver = usbDriver
        self.vendorId = vendorId
        self.productId = productId
        self.serialNumber = serialNumber
        self.speed = speed
        self.bsdInterface = bsdInterface
        self.networkTarget = networkTarget
        self.ipAddress = ipAddress
        self.subnetMask = subnetMask
        self.gateway = gateway
        self.macAddress = macAddress
        self.status = status
        self.driverType = driverType
    }

    /// Stable identifier for a dongle (no BSD interface). Used to remember a
    /// selected dongle target across refreshes.
    public static func dongleId(_ node: HardwareTopologyNode) -> String {
        "\(node.vendorId):\(node.productId):\(node.serialNumber)"
    }

    public var category: DeviceCategory {
        if status.localizedCaseInsensitiveContains("storage") || usbDriver.localizedCaseInsensitiveContains("storage") {
            return .storageMode
        }
        if usbDriver.localizedCaseInsensitiveContains("apple silicon") ||
           usbDriver.localizedCaseInsensitiveContains("broadcom") ||
           usbDriver.localizedCaseInsensitiveContains("built-in") {
            return .appleSilicon
        }
        if usbDriver.localizedCaseInsensitiveContains("thunderbolt") {
            return .thunderbolt
        }
        if usbDriver.localizedCaseInsensitiveContains("wifi") ||
           usbDriver.localizedCaseInsensitiveContains("wlan") ||
           usbDriver.localizedCaseInsensitiveContains("aic8800") {
            return .usbWiFiDongle
        }
        if usbDriver.localizedCaseInsensitiveContains("ethernet") ||
           usbDriver.localizedCaseInsensitiveContains("lan") ||
           usbDriver.localizedCaseInsensitiveContains("rtl8156") {
            return .ethernet
        }
        return .generic
    }

    public var isDefaultRoute: Bool {
        status.localizedCaseInsensitiveContains("default route")
    }

    public var isStorageMode: Bool {
        category == .storageMode
    }

    public var routeBadge: String {
        if isDefaultRoute {
            return "DEFAULT ROUTE"
        }
        if isStorageMode {
            return "STORAGE MODE"
        }
        if !networkTarget.isEmpty {
            return "CONNECTED"
        }
        if !bsdInterface.isEmpty {
            return "STANDBY"
        }
        return "DISCOVERED"
    }
}

public enum DeviceCategory: String, Sendable, Codable, CaseIterable {
    case appleSilicon = "Built-in Wi-Fi"
    case usbWiFiDongle = "USB Wi-Fi Dongle"
    case ethernet = "Ethernet LAN"
    case thunderbolt = "Thunderbolt Bridge"
    case storageMode = "Storage Mode (ZeroCD)"
    case generic = "Network Device"

    public var systemIconName: String {
        switch self {
        case .appleSilicon: return "laptopcomputer"
        case .usbWiFiDongle: return "antenna.radiowaves.left.and.right"
        case .ethernet: return "cable.connector.horizontal"
        case .thunderbolt: return "bolt.horizontal.fill"
        case .storageMode: return "externaldrive.badge.wifi"
        case .generic: return "cpu"
        }
    }

    public var shortLabel: String {
        switch self {
        case .appleSilicon: return "Built-in"
        case .usbWiFiDongle: return "USB Wi-Fi"
        case .ethernet: return "Ethernet"
        case .thunderbolt: return "Thunderbolt"
        case .storageMode: return "ZeroCD"
        case .generic: return "USB"
        }
    }
}

public struct PingResult: Identifiable, Codable, Sendable, Equatable {
    public var id: String { target }
    public let target: String
    public let isReachable: Bool
    public let rttMs: Int64
    public let packetLossPercent: Double

    enum CodingKeys: String, CodingKey {
        case target
        case isReachable = "is_reachable"
        case rttMs = "rtt_ms"
        case packetLossPercent = "packet_loss_percent"
    }

    public init(target: String, isReachable: Bool, rttMs: Int64, packetLossPercent: Double) {
        self.target = target
        self.isReachable = isReachable
        self.rttMs = rttMs
        self.packetLossPercent = packetLossPercent
    }
}

public struct StabilityStats: Codable, Sendable, Equatable {
    public let uptimeSeconds: Int64
    public let uptimeFormatted: String
    public let disconnectCount: Int
    public let reconnectCount: Int
    public let stabilityScore: Double
    public let currentStatus: String

    enum CodingKeys: String, CodingKey {
        case uptimeSeconds = "uptime_seconds"
        case uptimeFormatted = "uptime_formatted"
        case disconnectCount = "disconnect_count"
        case reconnectCount = "reconnect_count"
        case stabilityScore = "stability_score_percent"
        case currentStatus = "current_status"
    }

    public init(uptimeSeconds: Int64, uptimeFormatted: String, disconnectCount: Int, reconnectCount: Int, stabilityScore: Double, currentStatus: String) {
        self.uptimeSeconds = uptimeSeconds
        self.uptimeFormatted = uptimeFormatted
        self.disconnectCount = disconnectCount
        self.reconnectCount = reconnectCount
        self.stabilityScore = stabilityScore
        self.currentStatus = currentStatus
    }
}

public struct SpeedTestResult: Codable, Sendable, Equatable {
    public let interface: String
    public let downloadMbps: Double
    public let uploadMbps: Double
    public let latencyMs: Int64
    public let bytesDownloaded: Int64
    public let bytesUploaded: Int64
    public let durationSec: Double
    public let status: String

    enum CodingKeys: String, CodingKey {
        case interface
        case downloadMbps = "download_mbps"
        case uploadMbps = "upload_mbps"
        case latencyMs = "latency_ms"
        case bytesDownloaded = "bytes_downloaded"
        case bytesUploaded = "bytes_uploaded"
        case durationSec = "duration_sec"
        case status
    }

    public init(interface: String, downloadMbps: Double, uploadMbps: Double, latencyMs: Int64, bytesDownloaded: Int64, bytesUploaded: Int64, durationSec: Double, status: String) {
        self.interface = interface
        self.downloadMbps = downloadMbps
        self.uploadMbps = uploadMbps
        self.latencyMs = latencyMs
        self.bytesDownloaded = bytesDownloaded
        self.bytesUploaded = bytesUploaded
        self.durationSec = durationSec
        self.status = status
    }
}

public struct HTTPProbeResult: Identifiable, Codable, Sendable, Equatable {
    public var id: String { target }
    public let target: String
    public let url: String
    public let statusCode: Int
    public let dnsLookupMs: Int64
    public let tcpHandshakeMs: Int64
    public let tlsHandshakeMs: Int64
    public let ttfbMs: Int64
    public let totalMs: Int64
    public let isSuccess: Bool
    public let protocolName: String

    enum CodingKeys: String, CodingKey {
        case target
        case url
        case statusCode = "status_code"
        case dnsLookupMs = "dns_lookup_ms"
        case tcpHandshakeMs = "tcp_handshake_ms"
        case tlsHandshakeMs = "tls_handshake_ms"
        case ttfbMs = "ttfb_ms"
        case totalMs = "total_ms"
        case isSuccess = "is_success"
        case protocolName = "protocol"
    }

    public init(target: String, url: String, statusCode: Int, dnsLookupMs: Int64, tcpHandshakeMs: Int64, tlsHandshakeMs: Int64, ttfbMs: Int64, totalMs: Int64, isSuccess: Bool, protocolName: String) {
        self.target = target
        self.url = url
        self.statusCode = statusCode
        self.dnsLookupMs = dnsLookupMs
        self.tcpHandshakeMs = tcpHandshakeMs
        self.tlsHandshakeMs = tlsHandshakeMs
        self.ttfbMs = ttfbMs
        self.totalMs = totalMs
        self.isSuccess = isSuccess
        self.protocolName = protocolName
    }
}

public struct DNSProbeResult: Identifiable, Codable, Sendable, Equatable {
    public var id: String { domain }
    public let domain: String
    public let resolveTimeMs: Int64
    public let ips: [String]
    public let isSuccess: Bool
    public let server: String

    enum CodingKeys: String, CodingKey {
        case domain
        case resolveTimeMs = "resolve_time_ms"
        case ips
        case isSuccess = "is_success"
        case server
    }

    public init(domain: String, resolveTimeMs: Int64, ips: [String], isSuccess: Bool, server: String) {
        self.domain = domain
        self.resolveTimeMs = resolveTimeMs
        self.ips = ips
        self.isSuccess = isSuccess
        self.server = server
    }
}

public struct DiagnosticSuiteReport: Codable, Sendable, Equatable {
    public let iface: String
    public let localIp: String
    public let gateway: String
    public let pings: [PingResult]
    public let httpProbes: [HTTPProbeResult]
    public let dnsProbes: [DNSProbeResult]
    public let jitterMs: Double
    public let avgLatencyMs: Double
    public let minLatencyMs: Int64
    public let maxLatencyMs: Int64
    public let packetLossPercent: Double
    public let qualityScore: Double
    public let qualityGrade: String
    public let timestamp: String

    enum CodingKeys: String, CodingKey {
        case iface = "interface"
        case localIp = "local_ip"
        case gateway
        case pings
        case httpProbes = "http_probes"
        case dnsProbes = "dns_probes"
        case jitterMs = "jitter_ms"
        case avgLatencyMs = "avg_latency_ms"
        case minLatencyMs = "min_latency_ms"
        case maxLatencyMs = "max_latency_ms"
        case packetLossPercent = "packet_loss_percent"
        case qualityScore = "quality_score"
        case qualityGrade = "quality_grade"
        case timestamp
    }

    public init(iface: String, localIp: String, gateway: String, pings: [PingResult], httpProbes: [HTTPProbeResult], dnsProbes: [DNSProbeResult], jitterMs: Double, avgLatencyMs: Double, minLatencyMs: Int64, maxLatencyMs: Int64, packetLossPercent: Double, qualityScore: Double, qualityGrade: String, timestamp: String) {
        self.iface = iface
        self.localIp = localIp
        self.gateway = gateway
        self.pings = pings
        self.httpProbes = httpProbes
        self.dnsProbes = dnsProbes
        self.jitterMs = jitterMs
        self.avgLatencyMs = avgLatencyMs
        self.minLatencyMs = minLatencyMs
        self.maxLatencyMs = maxLatencyMs
        self.packetLossPercent = packetLossPercent
        self.qualityScore = qualityScore
        self.qualityGrade = qualityGrade
        self.timestamp = timestamp
    }
}

public struct ChipsetDeviceID: Identifiable, Codable, Sendable, Equatable {
    public var id: String { "\(vendorHex):\(productHex)" }
    public let vid: UInt16
    public let pid: UInt16
    public let vendorHex: String
    public let productHex: String
    public let productName: String
    public let manufacturer: String
    public let isStorage: Bool

    enum CodingKeys: String, CodingKey {
        case vid
        case pid
        case vendorHex = "vendor_hex"
        case productHex = "product_hex"
        case productName = "product_name"
        case manufacturer
        case isStorage = "is_storage"
    }

    public init(vid: UInt16, pid: UInt16, vendorHex: String, productHex: String, productName: String, manufacturer: String, isStorage: Bool) {
        self.vid = vid
        self.pid = pid
        self.vendorHex = vendorHex
        self.productHex = productHex
        self.productName = productName
        self.manufacturer = manufacturer
        self.isStorage = isStorage
    }
}

public struct ChipsetInfo: Identifiable, Codable, Sendable, Equatable {
    public var id: String { family }
    public let family: String
    public let chipsetName: String
    public let vendor: String
    public let standard: String
    public let maxSpeedMbps: Int
    public let supportedIds: [ChipsetDeviceID]
    public let capabilities: [String]
    public let driverState: String
    public let description: String

    enum CodingKeys: String, CodingKey {
        case family
        case chipsetName = "chipset_name"
        case vendor
        case standard
        case maxSpeedMbps = "max_speed_mbps"
        case supportedIds = "supported_ids"
        case capabilities
        case driverState = "driver_state"
        case description
    }

    public init(family: String, chipsetName: String, vendor: String, standard: String, maxSpeedMbps: Int, supportedIds: [ChipsetDeviceID], capabilities: [String], driverState: String, description: String) {
        self.family = family
        self.chipsetName = chipsetName
        self.vendor = vendor
        self.standard = standard
        self.maxSpeedMbps = maxSpeedMbps
        self.supportedIds = supportedIds
        self.capabilities = capabilities
        self.driverState = driverState
        self.description = description
    }
}

public struct InstallStep: Identifiable, Codable, Sendable, Equatable {
    public var id: Int { index }
    public let index: Int
    public let name: String
    public let description: String
    public let state: String
    public let details: String?
    public let durationMs: Int64

    enum CodingKeys: String, CodingKey {
        case index
        case name
        case description
        case state
        case details
        case durationMs = "duration_ms"
    }

    public init(index: Int, name: String, description: String, state: String, details: String? = nil, durationMs: Int64 = 0) {
        self.index = index
        self.name = name
        self.description = description
        self.state = state
        self.details = details
        self.durationMs = durationMs
    }
}

public struct DriverInstallProgress: Codable, Sendable, Equatable {
    public let isActive: Bool
    public let deviceName: String
    public let chipset: String
    public let currentStep: Int
    public let totalSteps: Int
    public let percent: Int
    public let steps: [InstallStep]
    public let logs: [String]
    public let error: String?
    public let isSuccess: Bool
    public let startedAt: String
    public let completedAt: String?

    enum CodingKeys: String, CodingKey {
        case isActive = "is_active"
        case deviceName = "device_name"
        case chipset
        case currentStep = "current_step"
        case totalSteps = "total_steps"
        case percent
        case steps
        case logs
        case error
        case isSuccess = "is_success"
        case startedAt = "started_at"
        case completedAt = "completed_at"
    }

    public init(isActive: Bool, deviceName: String, chipset: String, currentStep: Int, totalSteps: Int, percent: Int, steps: [InstallStep], logs: [String], error: String? = nil, isSuccess: Bool = false, startedAt: String = "", completedAt: String? = nil) {
        self.isActive = isActive
        self.deviceName = deviceName
        self.chipset = chipset
        self.currentStep = currentStep
        self.totalSteps = totalSteps
        self.percent = percent
        self.steps = steps
        self.logs = logs
        self.error = error
        self.isSuccess = isSuccess
        self.startedAt = startedAt
        self.completedAt = completedAt
    }
}

public struct SupervisorEvent: Identifiable, Codable, Sendable, Equatable {
    public let id: Int64
    public let timestamp: String
    public let severity: String
    public let component: String
    public let message: String
    public let details: String?

    public init(id: Int64, timestamp: String, severity: String, component: String, message: String, details: String? = nil) {
        self.id = id
        self.timestamp = timestamp
        self.severity = severity
        self.component = component
        self.message = message
        self.details = details
    }
}

public struct SupervisorStatus: Codable, Sendable, Equatable {
    public let isRunning: Bool
    public let lastHealthTime: String
    public let hardwareUp: Bool
    public let tunUp: Bool
    public let healCount: Int
    public let events: [SupervisorEvent]

    enum CodingKeys: String, CodingKey {
        case isRunning = "is_running"
        case lastHealthTime = "last_health_time"
        case hardwareUp = "hardware_up"
        case tunUp = "tun_up"
        case healCount = "heal_count"
        case events
    }

    public init(isRunning: Bool, lastHealthTime: String, hardwareUp: Bool, tunUp: Bool, healCount: Int, events: [SupervisorEvent]) {
        self.isRunning = isRunning
        self.lastHealthTime = lastHealthTime
        self.hardwareUp = hardwareUp
        self.tunUp = tunUp
        self.healCount = healCount
        self.events = events
    }
}

public struct RFChannelInfo: Identifiable, Codable, Sendable, Equatable {
    public var id: String { "\(band)-\(channel)" }
    public let channel: Int
    public let band: String
    public let frequencyMhz: Int
    public let bssidCount: Int
    public let ssids: [String]
    public let avgRssi: Int
    public let congestionLevel: String
    public let score: Double
    public let isNonOverlapping: Bool

    enum CodingKeys: String, CodingKey {
        case channel, band
        case frequencyMhz = "frequency_mhz"
        case bssidCount = "bssid_count"
        case ssids
        case avgRssi = "avg_rssi"
        case congestionLevel = "congestion_level"
        case score
        case isNonOverlapping = "is_non_overlapping"
    }
}

public struct SpectrumReport: Codable, Sendable, Equatable {
    public let channels24GHz: [RFChannelInfo]
    public let channels5GHz: [RFChannelInfo]
    public let optimalChannel24GHz: Int
    public let optimalChannel5GHz: Int
    public let totalNetworks: Int
    public let recommendations: [String]

    enum CodingKeys: String, CodingKey {
        case channels24GHz = "channels_24ghz"
        case channels5GHz = "channels_5ghz"
        case optimalChannel24GHz = "optimal_channel_24ghz"
        case optimalChannel5GHz = "optimal_channel_5ghz"
        case totalNetworks = "total_networks"
        case recommendations
    }
}

public struct SpeedTestReport: Codable, Sendable, Equatable {
    public let phase: String
    public let progressPercent: Double
    public let downloadMbps: Double
    public let uploadMbps: Double
    public let pingMs: Int64
    public let jitterMs: Double
    public let bytesReceived: Int64
    public let bytesSent: Int64
    public let interface: String
    public let server: String
    public let timestamp: String
    public let isRunning: Bool

    enum CodingKeys: String, CodingKey {
        case phase
        case progressPercent = "progress_percent"
        case downloadMbps = "download_mbps"
        case uploadMbps = "upload_mbps"
        case pingMs = "ping_ms"
        case jitterMs = "jitter_ms"
        case bytesReceived = "bytes_received"
        case bytesSent = "bytes_sent"
        case interface, server, timestamp
        case isRunning = "is_running"
    }
}

public struct InterfaceRouteInfo: Identifiable, Codable, Sendable, Equatable {
    public var id: String { name }
    public let name: String
    public let ip: String
    public let gateway: String
    public let isDefault: Bool
    public let metric: Int
    public let isReachable: Bool
    public let description: String

    enum CodingKeys: String, CodingKey {
        case name, ip, gateway, metric, description
        case isDefault = "is_default"
        case isReachable = "is_reachable"
    }
}

public struct FailoverEvent: Identifiable, Codable, Sendable, Equatable {
    public var id: String { "\(timestamp)-\(fromIface)-\(toIface)" }
    public let timestamp: String
    public let fromIface: String
    public let toIface: String
    public let reason: String

    enum CodingKeys: String, CodingKey {
        case timestamp, reason
        case fromIface = "from_iface"
        case toIface = "to_iface"
    }
}

public struct RoutingPolicyReport: Codable, Sendable, Equatable {
    public let activeDefaultInterface: String
    public let autoFailoverEnabled: Bool
    public let interfaces: [InterfaceRouteInfo]
    public let recentEvents: [FailoverEvent]
    public let lastEvaluated: String

    enum CodingKeys: String, CodingKey {
        case activeDefaultInterface = "active_default_interface"
        case autoFailoverEnabled = "auto_failover_enabled"
        case interfaces
        case recentEvents = "recent_events"
        case lastEvaluated = "last_evaluated"
    }
}
