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
    public var id: String { bsdInterface + vendorId }
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
