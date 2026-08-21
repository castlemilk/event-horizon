import SwiftUI
import EventHorizonCore

public struct PerDongleActionPanel: View {
    let node: HardwareTopologyNode
    let stat: InterfaceStat?
    let pings: [PingResult]
    let onSwitchWiFi: () -> Void

    @State private var isRunningPingTest = false
    @State private var isRunningSpeedTest = false
    @State private var pingStatusText = "Ready to test connectivity"
    @State private var speedTestResultText = ""

    public init(node: HardwareTopologyNode, stat: InterfaceStat?, pings: [PingResult], onSwitchWiFi: @escaping () -> Void) {
        self.node = node
        self.stat = stat
        self.pings = pings
        self.onSwitchWiFi = onSwitchWiFi
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // Header: Dongle -> Interface -> Network
            HStack(spacing: 10) {
                Image(systemName: deviceIcon(node.usbDriver))
                    .font(.title2)
                    .foregroundStyle(Color.accentColor)

                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        Text(node.usbDriver)
                            .font(.headline)

                        Text(node.bsdInterface)
                            .font(.caption2.weight(.bold).monospaced())
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Color.accentColor.opacity(0.12))
                            .foregroundStyle(Color.accentColor)
                            .clipShape(RoundedRectangle(cornerRadius: 4))
                    }

                    Text("Connected to: \(node.networkTarget) (\(node.ipAddress))")
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                }

                Spacer()

                HStack(spacing: 4) {
                    Circle()
                        .fill(node.status.contains("Connected") || node.status.contains("Active") ? Color.green : Color.orange)
                        .frame(width: 8, height: 8)
                    Text(node.status)
                        .font(.caption2.weight(.bold))
                        .foregroundStyle(node.status.contains("Connected") || node.status.contains("Active") ? .green : .orange)
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(Color.secondary.opacity(0.12))
                .clipShape(Capsule())
            }

            Divider()

            // Quick Options & Diagnostics Actions Row for this Dongle
            HStack(spacing: 12) {
                // 1. Test Connectivity Button
                Button(action: runConnectivityTest) {
                    HStack(spacing: 4) {
                        if isRunningPingTest {
                            ProgressView()
                                .controlSize(.small)
                        } else {
                            Image(systemName: "stethoscope")
                        }
                        Text("Test Connectivity")
                    }
                    .font(.caption.weight(.medium))
                }
                .buttonStyle(.bordered)
                .disabled(isRunningPingTest)

                // 2. Speed Test Button
                Button(action: runSpeedTest) {
                    HStack(spacing: 4) {
                        if isRunningSpeedTest {
                            ProgressView()
                                .controlSize(.small)
                        } else {
                            Image(systemName: "gauge.with.dots.needle.bottom.50percent")
                        }
                        Text("Speed Test")
                    }
                    .font(.caption.weight(.medium))
                }
                .buttonStyle(.bordered)
                .disabled(isRunningSpeedTest)

                // 3. Switch Wi-Fi Network (If Wi-Fi Dongle)
                if node.usbDriver.localizedCaseInsensitiveContains("Wi-Fi") {
                    Button(action: onSwitchWiFi) {
                        HStack(spacing: 4) {
                            Image(systemName: "wifi.badge.plus")
                            Text("Switch Wi-Fi")
                        }
                        .font(.caption.weight(.medium))
                    }
                    .buttonStyle(.borderedProminent)
                }

                Spacer()
            }

            // Results Display Bar (If diagnostics or speed tests run)
            if !pingStatusText.isEmpty || !speedTestResultText.isEmpty {
                HStack(spacing: 16) {
                    if !pingStatusText.isEmpty {
                        HStack(spacing: 4) {
                            Image(systemName: "timer")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                            Text(pingStatusText)
                                .font(.caption2.monospaced())
                        }
                    }

                    if !speedTestResultText.isEmpty {
                        HStack(spacing: 4) {
                            Image(systemName: "arrow.down.arrow.up")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                            Text(speedTestResultText)
                                .font(.caption2.monospaced().weight(.bold))
                                .foregroundStyle(.green)
                        }
                    }
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .background(Color.secondary.opacity(0.08))
                .clipShape(RoundedRectangle(cornerRadius: 6))
            }

            // Per-Dongle Live Telemetry Strip
            if let s = stat {
                HStack(spacing: 16) {
                    DongleStatPill(label: "Download", value: formatSpeed(s.rxRateKBps), icon: "arrow.down.circle.fill", color: .green)
                    DongleStatPill(label: "Upload", value: formatSpeed(s.txRateKBps), icon: "arrow.up.circle.fill", color: .blue)
                    DongleStatPill(label: "Packets In/Out", value: "\(s.packetsIn) / \(s.packetsOut)", icon: "shippingbox.fill", color: .secondary)
                    DongleStatPill(label: "Link State", value: s.isUp ? "UP" : "DOWN", icon: "link", color: s.isUp ? .green : .red)
                }
                .padding(10)
                .background(Color(nsColor: .controlBackgroundColor))
                .clipShape(RoundedRectangle(cornerRadius: 6))
            }
        }
        .padding(14)
        .background(Color(nsColor: .windowBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .overlay(
            RoundedRectangle(cornerRadius: 10)
                .stroke(Color.secondary.opacity(0.15), lineWidth: 1)
        )
    }

    private func runConnectivityTest() {
        isRunningPingTest = true
        pingStatusText = "Pinging gateway \(node.gateway)..."
        let gateway = node.gateway
        Task {
            do {
                let url = URL(string: "http://127.0.0.1:8990/api/diagnostics/ping?interface=\(node.bsdInterface)&target=\(gateway)")
                let (data, _) = try await URLSession.shared.data(from: url!)
                if let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                   let list = json["data"] as? [[String: Any]],
                   let first = list.first,
                   let reachable = first["is_reachable"] as? Bool {
                    if reachable, let rtt = first["rtt_ms"] as? Int {
                        pingStatusText = "Gateway Reachable: \(rtt) ms RTT (0% Loss)"
                    } else {
                        pingStatusText = "Gateway unreachable"
                    }
                } else {
                    pingStatusText = "No ping data returned"
                }
            } catch {
                pingStatusText = "Ping failed: \(error.localizedDescription)"
            }
            isRunningPingTest = false
        }
    }

    private func runSpeedTest() {
        isRunningSpeedTest = true
        speedTestResultText = "Testing throughput..."

        Task {
            do {
                let url = URL(string: "http://127.0.0.1:8990/api/diagnostics/speedtest?interface=\(node.bsdInterface)")
                let (data, _) = try await URLSession.shared.data(from: url!)
                if let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                   let dict = json["data"] as? [String: Any],
                   let rx = dict["download_mbps"] as? Double,
                   let tx = dict["upload_mbps"] as? Double {
                    speedTestResultText = String(format: "Download: %.1f Mbps | Upload: %.1f Mbps", rx, tx)
                } else {
                    speedTestResultText = "Speed test returned no results"
                }
            } catch {
                speedTestResultText = "Speed test failed: \(error.localizedDescription)"
            }
            isRunningSpeedTest = false
        }
    }

    private func deviceIcon(_ name: String) -> String {
        if name.contains("Wi-Fi") { return "wifi" }
        if name.contains("Ethernet") || name.contains("LAN") { return "cable.connector" }
        return "cpu"
    }

    private func formatSpeed(_ kbps: Double) -> String {
        if kbps > 1024 {
            return String(format: "%.2f MB/s", kbps / 1024.0)
        }
        return String(format: "%.1f KB/s", kbps)
    }
}

struct DongleStatPill: View {
    let label: String
    let value: String
    let icon: String
    let color: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 3) {
                Image(systemName: icon)
                    .font(.system(size: 9))
                    .foregroundStyle(color)
                Text(label)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            Text(value)
                .font(.caption.weight(.bold).monospacedDigit())
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
