import SwiftUI

public struct StarlinkTelemetryView: View {
    let isReachable: Bool
    let pingMs: Int
    let activeSSID: String

    public init(isReachable: Bool, pingMs: Int, activeSSID: String) {
        self.isReachable = isReachable
        self.pingMs = pingMs
        self.activeSSID = activeSSID
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Image(systemName: "antenna.radiowaves.left.and.right")
                    .font(.title2)
                    .foregroundStyle(isReachable ? .green : .secondary)

                VStack(alignment: .leading, spacing: 2) {
                    Text("Starlink Dish Telemetry")
                        .font(.headline)
                    Text("Target API: 192.168.100.1:9200")
                        .font(.caption2.monospaced())
                        .foregroundStyle(.secondary)
                }

                Spacer()

                HStack(spacing: 4) {
                    Circle()
                        .fill(isReachable ? Color.green : Color.orange)
                        .frame(width: 8, height: 8)
                    Text(isReachable ? "ONLINE" : "STANDBY")
                        .font(.caption2.weight(.bold))
                        .foregroundStyle(isReachable ? .green : .orange)
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background((isReachable ? Color.green : Color.orange).opacity(0.12))
                .clipShape(Capsule())
            }

            Divider()

            HStack(spacing: 20) {
                TelemetryMetric(label: "Endpoint", value: activeSSID, icon: "network")
                TelemetryMetric(label: "Ping Latency", value: isReachable ? "\(pingMs) ms" : "--", icon: "timer")
                TelemetryMetric(label: "Interface", value: "utun / en14", icon: "cable.connector")
                TelemetryMetric(label: "Protocol", value: "gRPC / HTTP", icon: "bolt.fill")
            }
        }
        .padding(14)
        .background(Color(nsColor: .windowBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .shadow(color: Color.black.opacity(0.05), radius: 3, x: 0, y: 1)
    }
}

struct TelemetryMetric: View {
    let label: String
    let value: String
    let icon: String

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 4) {
                Image(systemName: icon)
                    .font(.system(size: 10))
                    .foregroundStyle(.secondary)
                Text(label)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            Text(value)
                .font(.callout.weight(.semibold))
        }
    }
}
