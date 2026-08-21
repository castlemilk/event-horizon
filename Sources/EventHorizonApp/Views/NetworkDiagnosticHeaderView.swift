import SwiftUI
import EventHorizonCore

public struct NetworkDiagnosticHeaderView: View {
    let activeEndpoint: String
    let ipAddress: String
    let isConnected: Bool
    let rttMs: Int64
    let uptimeFormatted: String
    let stabilityScore: Double
    let hotspots: [AccessPoint]
    let onSelectHotspot: (String) -> Void

    public init(
        activeEndpoint: String,
        ipAddress: String,
        isConnected: Bool,
        rttMs: Int64,
        uptimeFormatted: String,
        stabilityScore: Double,
        hotspots: [AccessPoint] = [],
        onSelectHotspot: @escaping (String) -> Void = { _ in }
    ) {
        self.activeEndpoint = activeEndpoint
        self.ipAddress = ipAddress
        self.isConnected = isConnected
        self.rttMs = rttMs
        self.uptimeFormatted = uptimeFormatted
        self.stabilityScore = stabilityScore
        self.hotspots = hotspots
        self.onSelectHotspot = onSelectHotspot
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Image(systemName: isConnected ? "checkmark.seal.fill" : "exclamationmark.shield.fill")
                    .font(.title2)
                    .foregroundStyle(isConnected ? .green : .orange)

                VStack(alignment: .leading, spacing: 2) {
                    Text(isConnected ? "Active Connection Validated" : "Awaiting Network Validation")
                        .font(.headline)
                    Text("Endpoint: \(activeEndpoint) (\(ipAddress))")
                        .font(.caption2.monospaced())
                        .foregroundStyle(.secondary)
                }

                Spacer()

                // Interactive Dongle Wi-Fi Switcher Menu
                Menu {
                    Text("Select USB Dongle Target:")
                        .font(.caption)

                    Divider()

                    ForEach(hotspots) { ap in
                        Button(action: {
                            onSelectHotspot(ap.ssid)
                        }) {
                            HStack {
                                Text(ap.ssid)
                                if ap.isSelected {
                                    Text("✓ (Connected)")
                                }
                                Text("(\(ap.security))")
                            }
                        }
                    }

                    if hotspots.isEmpty {
                        Text("No in-range Wi-Fi networks found")
                    }
                } label: {
                    Label("Switch Wi-Fi Target", systemImage: "wifi.badge.plus")
                        .font(.caption.weight(.medium))
                }
                .menuStyle(.borderedButton)

                HStack(spacing: 4) {
                    Circle()
                        .fill(isConnected ? Color.green : Color.orange)
                        .frame(width: 8, height: 8)
                    Text(isConnected ? "ONLINE" : "DISCONNECTED")
                        .font(.caption2.weight(.bold))
                        .foregroundStyle(isConnected ? .green : .orange)
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background((isConnected ? Color.green : Color.orange).opacity(0.12))
                .clipShape(Capsule())
            }

            Divider()

            HStack(spacing: 20) {
                MetricPill(label: "Ping Latency (RTT)", value: rttMs > 0 ? "\(rttMs) ms" : "--", icon: "timer")
                MetricPill(label: "Link Uptime", value: uptimeFormatted, icon: "clock.fill")
                MetricPill(label: "Stability Score", value: String(format: "%.0f%%", stabilityScore), icon: "heart.text.square.fill")
                MetricPill(label: "OTel Metrics API", value: ":8990/metrics", icon: "chart.bar.fill")
            }
        }
        .padding(14)
        .background(Color(nsColor: .windowBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .shadow(color: Color.black.opacity(0.05), radius: 3, x: 0, y: 1)
    }
}

struct MetricPill: View {
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
                .font(.callout.weight(.semibold).monospaced())
        }
    }
}
