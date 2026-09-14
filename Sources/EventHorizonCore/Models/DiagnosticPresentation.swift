import Foundation

public extension PingResult {
    var summary: String {
        if let error, !error.isEmpty { return "\(target): \(error)" }
        let loss = String(format: "%.1f%% loss", packetLossPercent)
        guard isReachable else { return "\(target): unreachable · \(loss)" }
        let latency = rttMs >= 0 ? "\(rttMs) ms" : "latency unavailable"
        return "\(target): \(latency) · \(loss)"
    }
}

public extension SpeedTestResult {
    var hasMeasuredDownload: Bool { bytesDownloaded > 0 && downloadMbps.isFinite && downloadMbps >= 0 }
    var hasMeasuredUpload: Bool { bytesUploaded > 0 && uploadMbps.isFinite && uploadMbps >= 0 }
}
