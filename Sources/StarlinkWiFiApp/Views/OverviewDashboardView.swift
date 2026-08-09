import SwiftUI
import StarlinkWiFiCore

public struct OverviewDashboardView: View {
    let node: HardwareTopologyNode?
    let otherNodes: [HardwareTopologyNode]
    let stat: InterfaceStat?

    public init(node: HardwareTopologyNode?, otherNodes: [HardwareTopologyNode], stat: InterfaceStat?) {
        self.node = node
        self.otherNodes = otherNodes
        self.stat = stat
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            // Active Hardware Device Hero Card
            if let active = node {
                HStack(spacing: 24) {
                    // Render Device-Specific Hero Graphic
                    DeviceHeroGraphicView(driverName: active.usbDriver)
                        .frame(width: 140, height: 160)

                    // Right Device Controls & Telemetry
                    VStack(alignment: .leading, spacing: 14) {
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                HStack(spacing: 10) {
                                    Text(active.usbDriver)
                                        .font(.title2.weight(.bold))

                                    Text("Connected")
                                        .font(.caption.weight(.semibold))
                                        .padding(.horizontal, 8)
                                        .padding(.vertical, 3)
                                        .background(Color.green.opacity(0.18))
                                        .foregroundStyle(.green)
                                        .clipShape(Capsule())
                                }

                                Text("Device interface \(active.bsdInterface) operating normally.")
                                    .font(.subheadline)
                                    .foregroundStyle(.secondary)
                            }
                            Spacer()
                        }

                        // Metric Row Grid (Deduplicated)
                        HStack(spacing: 24) {
                            MetricSummaryTile(icon: "network", label: "IP address", value: active.ipAddress)
                            MetricSummaryTile(icon: "antenna.radiowaves.left.and.right", label: "Band", value: "5 GHz (Wi-Fi 6)")
                            MetricSummaryTile(icon: "waveform.path", label: "Channel", value: "149")
                            MetricSummaryTile(icon: "gauge", label: "Link speed", value: active.speed.contains("480") ? "480 Mbps" : "1200 Mbps")
                        }

                        Divider()

                        // Dual Line Throughput Chart & Monospaced Rates
                        HStack(alignment: .bottom, spacing: 16) {
                            LiveThroughputChartView(
                                rxData: [110, 140, 130, 175, 200, 185, 230, 215, stat?.rxRateKBps ?? 312],
                                txData: [28, 42, 38, 48, 58, 52, 60, 56, stat?.txRateKBps ?? 68]
                            )

                            VStack(alignment: .trailing, spacing: 6) {
                                HStack(spacing: 4) {
                                    Image(systemName: "arrow.down")
                                        .font(.caption.weight(.bold))
                                        .foregroundStyle(.blue)
                                    Text("\(Int(stat?.rxRateKBps ?? 312)) Mbps")
                                        .font(.title3.weight(.bold).monospacedDigit())
                                }

                                HStack(spacing: 4) {
                                    Image(systemName: "arrow.up")
                                        .font(.caption.weight(.bold))
                                        .foregroundStyle(.teal)
                                    Text("\(Int(stat?.txRateKBps ?? 68)) Mbps")
                                        .font(.body.weight(.bold).monospacedDigit())
                                }
                            }
                            .frame(width: 110, alignment: .trailing)
                        }

                        // Card Footer Action Buttons
                        HStack(spacing: 12) {
                            Button("Disconnect") {
                                // Disconnect action
                            }
                            .buttonStyle(.borderedProminent)
                            .tint(.blue)

                            Button(action: {}) {
                                Image(systemName: "gearshape")
                                    .font(.body)
                            }
                            .buttonStyle(.bordered)
                        }
                    }
                }
                .padding(20)
                .background(Color(nsColor: .windowBackgroundColor))
                .clipShape(RoundedRectangle(cornerRadius: 12))
                .overlay(
                    RoundedRectangle(cornerRadius: 12)
                        .stroke(Color.secondary.opacity(0.12), lineWidth: 1)
                )
                .shadow(color: Color.black.opacity(0.04), radius: 4, x: 0, y: 2)
            }

            // Other Devices Section
            VStack(alignment: .leading, spacing: 10) {
                Text("Other Attached Hardware")
                    .font(.headline)

                VStack(spacing: 8) {
                    ForEach(otherNodes) { device in
                        OtherDeviceRow(device: device)
                    }
                }
            }
        }
    }
}

struct MetricSummaryTile: View {
    let icon: String
    let label: String
    let value: String

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 4) {
                Image(systemName: icon)
                    .font(.caption2)
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

struct OtherDeviceRow: View {
    let device: HardwareTopologyNode

    var body: some View {
        HStack {
            Image(systemName: deviceIcon(device.usbDriver))
                .font(.title3)
                .foregroundStyle(.secondary)
                .frame(width: 28)

            Text(device.usbDriver)
                .font(.body.weight(.medium))

            Spacer()

            Text("Connected")
                .font(.caption2.weight(.medium))
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(Color.green.opacity(0.15))
                .foregroundStyle(.green)
                .clipShape(Capsule())

            Spacer()

            Text("IP: \(device.ipAddress)")
                .font(.caption.monospaced())
                .foregroundStyle(.secondary)

            Spacer()

            Text(device.speed.contains("5.0") ? "1 Gbps" : "1200 Mbps")
                .font(.caption)
                .foregroundStyle(.secondary)

            HStack(spacing: 4) {
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(.green)
                    .font(.caption)
                Text("Driver up to date")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Image(systemName: "chevron.right")
                .font(.caption2)
                .foregroundStyle(.secondary)
                .padding(.leading, 8)
        }
        .padding(12)
        .background(Color(nsColor: .controlBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(Color.secondary.opacity(0.12), lineWidth: 1)
        )
    }

    private func deviceIcon(_ name: String) -> String {
        if name.contains("Apple Silicon") || name.contains("Built-in") || name.contains("Broadcom") { return "laptopcomputer" }
        if name.contains("Wi-Fi") { return "wifi" }
        if name.contains("Ethernet") { return "network" }
        return "point.3.connected.trianglepath.dotted"
    }
}

struct DeviceHeroGraphicView: View {
    let driverName: String

    var body: some View {
        if driverName.contains("Wi-Fi") && (driverName.contains("Dongle") || driverName.contains("USB") || driverName.contains("AIC")) {
            // USB Wi-Fi Dongle Graphic
            USBDongleVectorView()
        } else if driverName.contains("Apple Silicon") || driverName.contains("Built-in") || driverName.contains("Broadcom") {
            // Render Generated Apple M-Series Chip Image Asset
            AppleSiliconChipGraphicView()
        } else {
            // Ethernet Adapter Graphic
            ZStack {
                RoundedRectangle(cornerRadius: 14)
                    .fill(LinearGradient(colors: [Color.blue.opacity(0.85), Color.blue], startPoint: .topLeading, endPoint: .bottomTrailing))
                VStack(spacing: 8) {
                    Image(systemName: "cable.connector.horizontal")
                        .font(.system(size: 44))
                        .foregroundStyle(.white)
                    Text("2.5G Ethernet")
                        .font(.caption.weight(.bold))
                        .foregroundStyle(.white.opacity(0.9))
                }
            }
        }
    }
}

struct AppleSiliconChipGraphicView: View {
    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 14)
                .fill(
                    LinearGradient(
                        colors: [Color(white: 0.20), Color(white: 0.10)],
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    )
                )
                .overlay(
                    RoundedRectangle(cornerRadius: 14)
                        .stroke(Color.cyan.opacity(0.3), lineWidth: 1)
                )

            VStack(spacing: 10) {
                ZStack {
                    Circle()
                        .fill(Color.cyan.opacity(0.15))
                        .frame(width: 60, height: 60)
                    Image(systemName: "cpu")
                        .font(.system(size: 32))
                        .foregroundStyle(Color.cyan)
                }

                Text("Apple Silicon")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(.white)

                Text("Wi-Fi 6E Engine")
                    .font(.system(size: 9))
                    .foregroundStyle(.secondary)
            }
        }
        .shadow(color: Color.cyan.opacity(0.2), radius: 8, x: 0, y: 4)
    }
}
