import SwiftUI
import StarlinkWiFiCore

public struct WiFiDiagnosticView: View {
    let pings: [PingResult]
    let stability: StabilityStats?

    public init(pings: [PingResult], stability: StabilityStats?) {
        self.pings = pings
        self.stability = stability
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            // Header
            HStack(spacing: 8) {
                Image(systemName: "stethoscope")
                    .font(.title2)
                    .foregroundStyle(.secondary)
                VStack(alignment: .leading, spacing: 2) {
                    Text("Connection Verification & Diagnostic Tools")
                        .font(.headline)
                    Text("Real-Time Ping Tests, RTT Latency, Packet Loss & OpenTelemetry (OTel) Exporter")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }

            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    // Uptime & Stability Card
                    if let stab = stability {
                        HStack(spacing: 12) {
                            DiagMetricTile(title: "Connection Uptime", value: stab.uptimeFormatted, subtitle: "Status: \(stab.currentStatus)", icon: "clock.arrow.2.circlepath", color: .green)
                            DiagMetricTile(title: "Link Stability Score", value: String(format: "%.1f%%", stab.stabilityScore), subtitle: "\(stab.disconnectCount) disconnects", icon: "shield.checkerboard", color: .blue)
                            DiagMetricTile(title: "Reconnection Count", value: "\(stab.reconnectCount)", subtitle: "Auto-healed links", icon: "arrow.triangle.2.circlepath", color: .purple)
                        }
                    }

                    // Ping Diagnostic Test Results Table
                    VStack(alignment: .leading, spacing: 10) {
                        Text("REACHABILITY & LATENCY PING TESTS")
                            .font(.caption2.weight(.bold))
                            .foregroundStyle(.secondary)

                        VStack(spacing: 6) {
                            ForEach(pings) { ping in
                                PingRow(ping: ping)
                            }
                        }
                    }

                    // OpenTelemetry Exporter Endpoint Info
                    HStack {
                        VStack(alignment: .leading, spacing: 3) {
                            HStack(spacing: 6) {
                                Image(systemName: "chart.bar.fill")
                                    .foregroundStyle(.purple)
                                Text("OpenTelemetry (OTel) Prometheus Metrics Exporter")
                                    .font(.body.weight(.semibold))
                            }
                            Text("Prometheus Scrape URL: http://127.0.0.1:8990/metrics")
                                .font(.caption2.monospaced())
                                .foregroundStyle(.secondary)
                        }

                        Spacer()

                        Button(action: {
                            if let url = URL(string: "http://127.0.0.1:8990/metrics") {
                                NSWorkspace.shared.open(url)
                            }
                        }) {
                            Label("Open /metrics", systemImage: "arrow.up.right.square")
                        }
                        .buttonStyle(.bordered)
                    }
                    .padding(12)
                    .background(Color.purple.opacity(0.08))
                    .clipShape(RoundedRectangle(cornerRadius: 8))
                    .overlay(
                        RoundedRectangle(cornerRadius: 8)
                            .stroke(Color.purple.opacity(0.2), lineWidth: 1)
                    )
                }
            }
        }
    }
}

struct PingRow: View {
    let ping: PingResult

    var body: some View {
        HStack {
            Image(systemName: ping.isReachable ? "checkmark.circle.fill" : "xmark.circle.fill")
                .foregroundStyle(ping.isReachable ? .green : .red)

            Text(ping.target)
                .font(.body.weight(.medium).monospaced())

            Spacer()

            if ping.isReachable {
                HStack(spacing: 6) {
                    Text("\(ping.rttMs) ms RTT")
                        .font(.callout.monospacedDigit().weight(.semibold))
                        .foregroundStyle(rttColor(ping.rttMs))

                    Text("(0% Loss)")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            } else {
                Text("PACKET LOSS (100%)")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(.red)
            }
        }
        .padding(10)
        .background(Color(nsColor: .controlBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 6))
        .overlay(
            RoundedRectangle(cornerRadius: 6)
                .stroke(Color.secondary.opacity(0.1), lineWidth: 1)
        )
    }

    private func rttColor(_ ms: Int64) -> Color {
        if ms < 30 { return .green }
        if ms < 100 { return .orange }
        return .red
    }
}

struct DiagMetricTile: View {
    let title: String
    let value: String
    let subtitle: String
    let icon: String
    let color: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 4) {
                Image(systemName: icon)
                    .font(.caption)
                    .foregroundStyle(color)
                Text(title)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            Text(value)
                .font(.title3.weight(.bold).monospacedDigit())
            Text(subtitle)
                .font(.caption2)
                .foregroundStyle(.secondary)
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(nsColor: .controlBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(Color.secondary.opacity(0.12), lineWidth: 1)
        )
    }
}
