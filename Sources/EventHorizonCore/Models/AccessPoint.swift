import Foundation

public struct AccessPoint: Identifiable, Codable, Sendable, Equatable {
    public var id: String { bssid.isEmpty ? "\(ssid)|\(channel)" : bssid }
    public let ssid: String
    public let bssid: String
    public let rssi: Int8
    public let channel: UInt8
    public let security: String
    public let isSelected: Bool

    enum CodingKeys: String, CodingKey {
        case ssid
        case bssid
        case rssi
        case channel
        case security
        case isSelected = "is_selected"
    }

    public init(ssid: String, bssid: String, rssi: Int8, channel: UInt8, security: String, isSelected: Bool = false) {
        self.ssid = ssid
        self.bssid = bssid
        self.rssi = rssi
        self.channel = channel
        self.security = security
        self.isSelected = isSelected
    }
}

public struct APIResponse<T: Codable & Sendable>: Codable, Sendable {
    public let status: String
    public let message: String?
    public let data: T?
}

public struct DaemonStatus: Codable, Sendable {
    public let version: String
    public let hotspots: Int
    public let arch: String
    public let os: String
}
