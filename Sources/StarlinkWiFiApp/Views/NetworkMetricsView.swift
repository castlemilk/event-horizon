import SwiftUI
import StarlinkWiFiCore

public struct NetworkMetricsView: View {
    let stats: [InterfaceStat]

    public init(stats: [InterfaceStat]) {
        self.stats = stats
    }

    private var activeStats: [InterfaceStat] {
        stats.filter { $0.bytesIn > 0 || $0.bytesOut > 0 }
    }

    private var dormantStats: [InterfaceStat] {
        stats.filter { $0.bytesIn == 0 && $0.bytesOut == 0 }
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            // Header
            HStack(spacing: 8) {
                Image(systemName: "chart.line.uptrend.xyaxis")
                    .font(.title2)
                    .foregroundStyle(.secondary)
                VStack(alignment: .leading, spacing: 2) {
                    Text("Interface Traffic & Telemetry")
                        .font(.headline)
                    Text("Real-time kernel network statistics and bandwidth throughput")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }

            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    // Active Interfaces Group
                    if !activeStats.isEmpty {
                        VStack(alignment: .leading, spacing: 10) {
                            Text("ACTIVE INTERFACES (\(activeStats.count))")
                                .font(.caption2.weight(.bold))
                                .foregroundStyle(.secondary)

                            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 12) {
                                ForEach(activeStats) { stat in
                                    InterfaceMetricCard(stat: stat, isActive: true)
                                }
                            }
                        }
                    }

                    // Dormant Interfaces Group
                    if !dormantStats.isEmpty {
                        VStack(alignment: .leading, spacing: 10) {
                            Text("DORMANT INTERFACES (\(dormantStats.count))")
                                .font(.caption2.weight(.bold))
                                .foregroundStyle(.secondary)

                            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible()), GridItem(.flexible())], spacing: 8) {
                                ForEach(dormantStats) { stat in
                                    InterfaceMetricCard(stat: stat, isActive: false)
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

struct InterfaceMetricCard: View {
    let stat: InterfaceStat
    let isActive: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(stat.name)
                    .font(.body.weight(.bold).monospaced())
                Spacer()
                HStack(spacing: 4) {
                    Circle()
                        .fill(stat.isUp ? Color.green : Color.secondary)
                        .frame(width: 6, height: 6)
                    Text(stat.isUp ? "UP" : "DOWN")
                        .font(.caption2.weight(.bold))
                        .foregroundStyle(stat.isUp ? .green : .secondary)
                }
            }

            if isActive {
                Divider()

                // Live Speed Rates
                HStack(spacing: 12) {
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 3) {
                            Image(systemName: "arrow.down.circle.fill")
                                .font(.caption)
                                .foregroundStyle(.green)
                            Text("Download")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                        Text(formatSpeed(stat.rxRateKBps))
                            .font(.callout.weight(.bold).monospacedDigit())
                    }

                    Spacer()

                    VStack(alignment: .trailing, spacing: 2) {
                        HStack(spacing: 3) {
                            Image(systemName: "arrow.up.circle.fill")
                                .font(.caption)
                                .foregroundStyle(.blue)
                            Text("Upload")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                        Text(formatSpeed(stat.txRateKBps))
                            .font(.callout.weight(.bold).monospacedDigit())
                    }
                }

                // Cumulative Statistics
                VStack(spacing: 3) {
                    MetricRow(label: "Packets In/Out", value: "\(formatNumber(stat.packetsIn)) / \(formatNumber(stat.packetsOut))")
                    MetricRow(label: "Data Volume", value: "\(formatBytes(stat.bytesIn)) / \(formatBytes(stat.bytesOut))")
                    MetricRow(label: "Errors In/Out", value: "\(stat.errorsIn) / \(stat.errorsOut)", isError: stat.errorsIn > 0 || stat.errorsOut > 0)
                }
            }
        }
        .padding(10)
        .background(Color(nsColor: .controlBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(isActive ? Color.accentColor.opacity(0.3) : Color.secondary.opacity(0.12), lineWidth: 1)
        )
    }

    private func formatSpeed(_ kbps: Double) -> String {
        if kbps > 1024 {
            return String(format: "%.2f MB/s", kbps / 1024.0)
        }
        return String(format: "%.1f KB/s", kbps)
    }

    private func formatBytes(_ bytes: UInt64) -> String {
        let mb = Double(bytes) / (1024.0 * 1024.0)
        if mb > 1024 {
            return String(format: "%.2f GB", mb / 1024.0)
        }
        return String(format: "%.1f MB", mb)
    }

    private func formatNumber(_ val: UInt64) -> String {
        if val > 1_000_000 {
            return String(format: "%.1fM", Double(val)/1_000_000.0)
        }
        if val > 1_000 {
            return String(format: "%.1fK", Double(val)/1_000.0)
        }
        return "\(val)"
    }
}

struct MetricRow: View {
    let label: String
    let value: String
    var isError: Bool = false

    var body: some View {
        HStack {
            Text(label)
                .font(.caption2)
                .foregroundStyle(.secondary)
            Spacer()
            Text(value)
                .font(.caption2.monospacedDigit())
                .foregroundStyle(isError ? Color.red : Color.primary)
        }
    }
}
