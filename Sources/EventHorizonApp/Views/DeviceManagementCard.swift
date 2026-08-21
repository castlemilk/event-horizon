import SwiftUI
import EventHorizonCore

public struct DeviceManagementCard: View {
    let node: HardwareTopologyNode
    let hotspots: [AccessPoint]
    let stat: InterfaceStat?
    let pings: [PingResult]
    let onSelectHotspot: (String) -> Void

    @State private var selectedSubTab = 0
    @State private var isRunningPingTest = false
    @State private var isRunningSpeedTest = false
    @State private var pingResultText = "Ready for test"
    @State private var speedTestResultText = ""

    public init(
        node: HardwareTopologyNode,
        hotspots: [AccessPoint],
        stat: InterfaceStat?,
        pings: [PingResult],
        onSelectHotspot: @escaping (String) -> Void
    ) {
        self.node = node
        self.hotspots = hotspots
        self.stat = stat
        self.pings = pings
        self.onSelectHotspot = onSelectHotspot
    }

    private var isWiFiDevice: Bool {
        node.usbDriver.localizedCaseInsensitiveContains("Wi-Fi") || node.usbDriver.localizedCaseInsensitiveContains("WLAN")
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            // Card Top Header: Device Name & Connection Badge
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

                    Text("Assigned Target: \(node.networkTarget) (\(node.ipAddress))")
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

            // Sub-Management Section Picker
            Picker("", selection: $selectedSubTab) {
                if isWiFiDevice {
                    Text("📡 Wi-Fi Hotspots (\(hotspots.count))").tag(0)
                    Text("📊 Metrics & Analytics").tag(1)
                    Text("📋 Details & Metadata").tag(2)
                } else {
                    Text("📊 Metrics & Analytics").tag(1)
                    Text("📋 Details & Metadata").tag(2)
                }
            }
            .pickerStyle(.segmented)

            // Sub-Section Content
            if selectedSubTab == 0 && isWiFiDevice {
                // 1. Nested Wi-Fi Hotspots View for this Device
                VStack(alignment: .leading, spacing: 8) {
                    HStack {
                        Text("IN-RANGE WI-FI HOTSPOTS FOR THIS CHIP")
                            .font(.caption2.weight(.bold))
                            .foregroundStyle(.secondary)
                        Spacer()
                    }

                    VStack(spacing: 6) {
                        ForEach(hotspots) { ap in
                            HStack {
                                Image(systemName: ap.isSelected ? "wifi.circle.fill" : "wifi")
                                    .font(.title3)
                                    .foregroundStyle(ap.isSelected ? .green : .secondary)

                                VStack(alignment: .leading, spacing: 2) {
                                    HStack(spacing: 6) {
                                        Text(ap.ssid)
                                            .font(.body.weight(.semibold))

                                        if ap.isSelected {
                                            Text("ACTIVE")
                                                .font(.system(size: 9, weight: .bold))
                                                .padding(.horizontal, 5)
                                                .padding(.vertical, 1)
                                                .background(Color.green.opacity(0.15))
                                                .foregroundStyle(.green)
                                                .clipShape(Capsule())
                                        }
                                    }
                                    Text("\(ap.bssid) • Channel \(ap.channel) • \(ap.security)")
                                        .font(.caption2.monospaced())
                                        .foregroundStyle(.secondary)
                                }

                                Spacer()

                                Button(action: {
                                    onSelectHotspot(ap.ssid)
                                }) {
                                    Text(ap.isSelected ? "Connected" : "Connect")
                                        .font(.caption.weight(.medium))
                                }
                                .buttonStyle(.borderedProminent)
                                .tint(ap.isSelected ? .green : .accentColor)
                                .disabled(ap.isSelected)
                            }
                            .padding(10)
                            .background(ap.isSelected ? Color.accentColor.opacity(0.08) : Color(nsColor: .controlBackgroundColor))
                            .clipShape(RoundedRectangle(cornerRadius: 6))
                            .overlay(
                                RoundedRectangle(cornerRadius: 6)
                                    .stroke(ap.isSelected ? Color.accentColor : Color.secondary.opacity(0.12), lineWidth: 1)
                            )
                        }

                        if hotspots.isEmpty {
                            Text("No Wi-Fi access points detected in range.")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .padding(8)
                        }
                    }
                }
            } else if selectedSubTab == 1 {
                // 2. Metrics & Analytics View for this Device
                VStack(alignment: .leading, spacing: 10) {
                    HStack(spacing: 12) {
                        Button(action: runPingTest) {
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

                        Spacer()
                    }

                    if !pingResultText.isEmpty || !speedTestResultText.isEmpty {
                        HStack(spacing: 16) {
                            if !pingResultText.isEmpty {
                                HStack(spacing: 4) {
                                    Image(systemName: "timer")
                                        .font(.caption2)
                                    Text(pingResultText)
                                        .font(.caption2.monospaced())
                                }
                            }
                            if !speedTestResultText.isEmpty {
                                HStack(spacing: 4) {
                                    Image(systemName: "arrow.down.arrow.up")
                                        .font(.caption2)
                                    Text(speedTestResultText)
                                        .font(.caption2.monospaced().weight(.bold))
                                        .foregroundStyle(.green)
                                }
                            }
                        }
                        .padding(8)
                        .background(Color.secondary.opacity(0.08))
                        .clipShape(RoundedRectangle(cornerRadius: 6))
                    }

                    if let s = stat {
                        HStack(spacing: 12) {
                            SubMetricTile(label: "Download Speed", value: formatSpeed(s.rxRateKBps), icon: "arrow.down.circle.fill", color: .green)
                            SubMetricTile(label: "Upload Speed", value: formatSpeed(s.txRateKBps), icon: "arrow.up.circle.fill", color: .blue)
                            SubMetricTile(label: "Packets In / Out", value: "\(s.packetsIn) / \(s.packetsOut)", icon: "shippingbox.fill", color: .secondary)
                            SubMetricTile(label: "Data Volume", value: "\(formatBytes(s.bytesIn)) / \(formatBytes(s.bytesOut))", icon: "square.stack.3d.up.fill", color: .purple)
                        }
                    }
                }
            } else {
                // 3. Details & Metadata View for this Device
                VStack(alignment: .leading, spacing: 8) {
                    Text("HARDWARE SPECIFICATIONS & METADATA")
                        .font(.caption2.weight(.bold))
                        .foregroundStyle(.secondary)

                    VStack(spacing: 6) {
                        DetailGridRow(label: "Vendor ID / Product ID", value: "\(node.vendorId) / \(node.productId)")
                        DetailGridRow(label: "Serial Number", value: node.serialNumber)
                        DetailGridRow(label: "Physical Bus Speed", value: node.speed)
                        DetailGridRow(label: "BSD Kernel Interface", value: node.bsdInterface)
                        DetailGridRow(label: "Assigned IP / Subnet Mask", value: "\(node.ipAddress) (\(node.subnetMask))")
                        DetailGridRow(label: "Default Gateway IP", value: node.gateway)
                        DetailGridRow(label: "MAC Address", value: node.macAddress)
                        DetailGridRow(label: "Driver Architecture", value: node.driverType)
                    }
                    .padding(10)
                    .background(Color(nsColor: .controlBackgroundColor))
                    .clipShape(RoundedRectangle(cornerRadius: 6))
                }
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

    private func runPingTest() {
        isRunningPingTest = true
        pingResultText = "Pinging \(node.gateway)..."
        let gateway = node.gateway
        Task {
            do {
                let url = URL(string: "http://127.0.0.1:8990/api/diagnostics/ping?interface=\(node.bsdInterface)&target=\(gateway)")
                let (data, _) = try await URLSession.shared.data(from: url!)
                if let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                   let list = json["data"] as? [[String: Any]],
                   let first = list.first,
                   let reachable = first["is_reachable"] as? Bool {
                    let rtt = first["rtt_ms"] as? Int
                    if reachable, let rtt {
                        pingResultText = "Gateway Reachable: \(rtt) ms RTT (0% Loss)"
                    } else {
                        pingResultText = "Gateway unreachable"
                    }
                } else {
                    pingResultText = "No ping data returned"
                }
            } catch {
                pingResultText = "Ping failed: \(error.localizedDescription)"
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
        if name.contains("Apple Silicon") || name.contains("Built-in") || name.contains("Broadcom") { return "laptopcomputer" }
        if name.contains("Wi-Fi") || name.contains("WLAN") { return "wifi" }
        if name.contains("Ethernet") || name.contains("LAN") { return "cable.connector" }
        return "cpu"
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
}

struct SubMetricTile: View {
    let label: String
    let value: String
    let icon: String
    let color: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
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
        .padding(8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(nsColor: .controlBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 6))
    }
}

struct DetailGridRow: View {
    let label: String
    let value: String

    var body: some View {
        HStack {
            Text(label)
                .font(.caption2)
                .foregroundStyle(.secondary)
            Spacer()
            Text(value)
                .font(.caption.monospaced())
        }
    }
}
