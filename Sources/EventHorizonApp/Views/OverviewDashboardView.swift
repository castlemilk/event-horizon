import SwiftUI
import EventHorizonCore

public struct OverviewDashboardView: View {
    let node: HardwareTopologyNode?
    let otherNodes: [HardwareTopologyNode]
    let stat: InterfaceStat?
    let activeHotspot: AccessPoint?
    let rxHistory: [Double]
    let txHistory: [Double]
    let onSelectDevice: (HardwareTopologyNode) -> Void
    let onDisconnect: () -> Void

    @State private var showVirtualBridges = false

    public init(
        node: HardwareTopologyNode?,
        otherNodes: [HardwareTopologyNode],
        stat: InterfaceStat?,
        activeHotspot: AccessPoint? = nil,
        rxHistory: [Double] = [],
        txHistory: [Double] = [],
        onSelectDevice: @escaping (HardwareTopologyNode) -> Void = { _ in },
        onDisconnect: @escaping () -> Void = {}
    ) {
        self.node = node
        self.otherNodes = otherNodes
        self.stat = stat
        self.activeHotspot = activeHotspot
        self.rxHistory = rxHistory
        self.txHistory = txHistory
        self.onSelectDevice = onSelectDevice
        self.onDisconnect = onDisconnect
    }

    private var observedHotspot: AccessPoint? {
        guard let spot = activeHotspot, !spot.ssid.isEmpty else { return nil }
        return spot
    }

    private var channelLabel: String {
        guard let spot = observedHotspot, spot.channel > 0 else { return "—" }
        return "\(spot.channel) (\(spot.channel > 14 ? "5 GHz" : "2.4 GHz"))"
    }

    private var signalLabel: String {
        guard let spot = observedHotspot, spot.rssi != 0 else { return "—" }
        return "\(spot.rssi) dBm"
    }

    private func networkLabel(_ node: HardwareTopologyNode) -> String {
        if let spot = observedHotspot, !spot.ssid.isEmpty {
            return spot.ssid
        }
        return node.networkTarget.isEmpty ? "—" : node.networkTarget
    }

    private var rxRateLabel: String {
        guard let rate = stat?.rxRateKBps, rate > 0 else { return "0 KB/s" }
        if rate >= 1024 {
            return String(format: "%.1f MB/s", rate / 1024.0)
        }
        return "\(Int(rate)) KB/s"
    }

    private var txRateLabel: String {
        guard let rate = stat?.txRateKBps, rate > 0 else { return "0 KB/s" }
        if rate >= 1024 {
            return String(format: "%.1f MB/s", rate / 1024.0)
        }
        return "\(Int(rate)) KB/s"
    }

    private var physicalOtherNodes: [HardwareTopologyNode] {
        otherNodes.filter { $0.category != .thunderbolt && !$0.bsdInterface.hasPrefix("vm") && !$0.bsdInterface.hasPrefix("bridge") }
    }

    private var virtualBridgeNodes: [HardwareTopologyNode] {
        otherNodes.filter { $0.category == .thunderbolt || $0.bsdInterface.hasPrefix("vm") || $0.bsdInterface.hasPrefix("bridge") }
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            // Active Hardware Device Hero Card
            if let active = node {
                VStack(alignment: .leading, spacing: 14) {
                    HStack(alignment: .top, spacing: 20) {
                        // Device Hero Icon & Category Graphic
                        DeviceHeroGraphicView(driverName: active.usbDriver, category: active.category)
                            .frame(width: 120, height: 130)

                        // Device Controls & Telemetry
                        VStack(alignment: .leading, spacing: 10) {
                            HStack {
                                VStack(alignment: .leading, spacing: 3) {
                                    HStack(spacing: 8) {
                                        Text(active.usbDriver)
                                            .font(.title3.weight(.bold))

                                        // Category Pill
                                        Text(active.category.rawValue)
                                            .font(.caption2.weight(.semibold))
                                            .padding(.horizontal, 7)
                                            .padding(.vertical, 2.5)
                                            .background(categoryBadgeColor(for: active.category).opacity(0.15))
                                            .foregroundStyle(categoryBadgeColor(for: active.category))
                                            .clipShape(Capsule())

                                        // Route Pill
                                        if active.isDefaultRoute {
                                            Text("Default Route")
                                                .font(.caption2.weight(.bold))
                                                .padding(.horizontal, 7)
                                                .padding(.vertical, 2.5)
                                                .background(Color.green.opacity(0.18))
                                                .foregroundStyle(.green)
                                                .clipShape(Capsule())
                                        }
                                    }

                                    HStack(spacing: 8) {
                                        if !active.bsdInterface.isEmpty {
                                            Text("Interface \(active.bsdInterface)")
                                                .font(.caption.monospaced())
                                                .foregroundStyle(.secondary)
                                        }
                                        if !active.macAddress.isEmpty {
                                            Text("• MAC: \(active.macAddress)")
                                                .font(.caption.monospaced())
                                                .foregroundStyle(.tertiary)
                                        }
                                        if !active.speed.isEmpty {
                                            Text("• \(active.speed)")
                                                .font(.caption)
                                                .foregroundStyle(.tertiary)
                                        }
                                    }
                                }
                                Spacer()
                            }

                            // Metric Row Grid
                            HStack(spacing: 20) {
                                MetricSummaryTile(icon: "network", label: "IP Address", value: active.ipAddress.isEmpty ? "—" : active.ipAddress)
                                MetricSummaryTile(icon: "antenna.radiowaves.left.and.right", label: "Connected SSID", value: networkLabel(active))
                                MetricSummaryTile(icon: "waveform.path", label: "Channel & Band", value: channelLabel)
                                MetricSummaryTile(icon: "signal", label: "Signal Strength", value: signalLabel)
                            }

                            Divider()

                            // Throughput Chart & Monospaced Rates
                            HStack(alignment: .bottom, spacing: 16) {
                                LiveThroughputChartView(
                                    rxData: rxHistory,
                                    txData: txHistory
                                )

                                VStack(alignment: .trailing, spacing: 6) {
                                    HStack(spacing: 4) {
                                        Image(systemName: "arrow.down")
                                            .font(.caption.weight(.bold))
                                            .foregroundStyle(.blue)
                                        Text(rxRateLabel)
                                            .font(.title3.weight(.bold).monospacedDigit())
                                    }

                                    HStack(spacing: 4) {
                                        Image(systemName: "arrow.up")
                                            .font(.caption.weight(.bold))
                                            .foregroundStyle(.teal)
                                        Text(txRateLabel)
                                            .font(.body.weight(.bold).monospacedDigit())
                                    }
                                }
                                .frame(width: 110, alignment: .trailing)
                            }

                            // Card Footer Action Buttons
                            HStack(spacing: 12) {
                                if !active.networkTarget.isEmpty || observedHotspot != nil {
                                    Button("Disconnect", action: onDisconnect)
                                        .buttonStyle(.borderedProminent)
                                        .tint(.blue)
                                } else {
                                    Button("Scan Available Networks", action: { onSelectDevice(active) })
                                        .buttonStyle(.borderedProminent)
                                        .tint(.blue)
                                }

                                Button(action: { onSelectDevice(active) }) {
                                    HStack(spacing: 4) {
                                        Image(systemName: "slider.horizontal.3")
                                        Text("Device Details")
                                    }
                                }
                                .buttonStyle(.bordered)
                            }
                        }
                    }
                }
                .padding(18)
                .background(Color(nsColor: .windowBackgroundColor))
                .clipShape(RoundedRectangle(cornerRadius: 12))
                .overlay(
                    RoundedRectangle(cornerRadius: 12)
                        .stroke(Color.secondary.opacity(0.12), lineWidth: 1)
                )
            }

            // Other Physical Devices Section
            VStack(alignment: .leading, spacing: 10) {
                HStack {
                    Text("ATTACHED NETWORK ADAPTERS")
                        .font(.system(size: 11, weight: .bold))
                        .foregroundStyle(.secondary)

                    Spacer()

                    Text("\(physicalOtherNodes.count + (node != nil ? 1 : 0)) Active/Physical")
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                }

                VStack(spacing: 8) {
                    ForEach(physicalOtherNodes) { device in
                        Button(action: { onSelectDevice(device) }) {
                            OtherDeviceRow(device: device)
                        }
                        .buttonStyle(.plain)
                    }

                    if physicalOtherNodes.isEmpty {
                        if node != nil {
                            HStack(spacing: 8) {
                                Image(systemName: "checkmark.circle")
                                    .foregroundStyle(.secondary)
                                Text("No additional physical USB adapters attached.")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                            .padding(.vertical, 4)
                        } else {
                            Text("No network interfaces detected by daemon.")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .padding(16)
                        }
                    }
                }
            }

            // Collapsible Inactive / Virtual Bridges Section
            if !virtualBridgeNodes.isEmpty {
                DisclosureGroup(
                    isExpanded: $showVirtualBridges,
                    content: {
                        VStack(spacing: 6) {
                            ForEach(virtualBridgeNodes) { device in
                                Button(action: { onSelectDevice(device) }) {
                                    OtherDeviceRow(device: device)
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .padding(.top, 6)
                    },
                    label: {
                        HStack {
                            Image(systemName: "bolt.horizontal")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                            Text("Virtual & Thunderbolt Bridges (\(virtualBridgeNodes.count))")
                                .font(.caption.weight(.medium))
                                .foregroundStyle(.secondary)
                        }
                    }
                )
                .padding(.top, 4)
            }
        }
    }

    private func categoryBadgeColor(for category: DeviceCategory) -> Color {
        switch category {
        case .appleSilicon: return .blue
        case .usbWiFiDongle: return .purple
        case .ethernet: return .teal
        case .thunderbolt: return .secondary
        case .storageMode: return .orange
        case .generic: return .secondary
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
                    .font(.system(size: 10))
                    .foregroundStyle(.secondary)
                Text(label)
                    .font(.system(size: 10))
                    .foregroundStyle(.secondary)
            }
            Text(value)
                .font(.system(size: 12, weight: .semibold).monospaced())
                .lineLimit(1)
        }
    }
}

struct OtherDeviceRow: View {
    let device: HardwareTopologyNode

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: device.category.systemIconName)
                .font(.title3)
                .foregroundStyle(iconColor(for: device.category))
                .frame(width: 28)

            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(device.usbDriver)
                        .font(.body.weight(.medium))

                    if !device.bsdInterface.isEmpty {
                        Text(device.bsdInterface)
                            .font(.caption2.weight(.bold).monospaced())
                            .padding(.horizontal, 5)
                            .padding(.vertical, 1)
                            .background(Color.secondary.opacity(0.12))
                            .foregroundStyle(.primary)
                            .clipShape(RoundedRectangle(cornerRadius: 3))
                    }

                    Text("[\(device.category.shortLabel)]")
                        .font(.caption2.weight(.medium))
                        .foregroundStyle(.secondary)
                }

                HStack(spacing: 6) {
                    if !device.networkTarget.isEmpty {
                        HStack(spacing: 3) {
                            Image(systemName: "wifi")
                                .font(.system(size: 9))
                                .foregroundStyle(.blue)
                            Text(device.networkTarget)
                                .font(.caption.weight(.medium))
                                .foregroundStyle(.secondary)
                        }
                    } else {
                        Text(device.status)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }

                    if !device.ipAddress.isEmpty {
                        Text("• \(device.ipAddress)")
                            .font(.caption.monospaced())
                            .foregroundStyle(.secondary)
                    }
                }
            }

            Spacer()

            if device.isDefaultRoute {
                Text("Default Route")
                    .font(.caption2.weight(.bold))
                    .padding(.horizontal, 7)
                    .padding(.vertical, 2)
                    .background(Color.green.opacity(0.15))
                    .foregroundStyle(.green)
                    .clipShape(Capsule())
            } else if device.isStorageMode {
                Text("ZeroCD Storage")
                    .font(.caption2.weight(.bold))
                    .padding(.horizontal, 7)
                    .padding(.vertical, 2)
                    .background(Color.orange.opacity(0.15))
                    .foregroundStyle(.orange)
                    .clipShape(Capsule())
            } else if !device.networkTarget.isEmpty {
                Text("Connected")
                    .font(.caption2.weight(.medium))
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(Color.secondary.opacity(0.12))
                    .foregroundStyle(.primary)
                    .clipShape(Capsule())
            }

            if !device.speed.isEmpty {
                Text(device.speed)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Image(systemName: "chevron.right")
                .font(.caption2)
                .foregroundStyle(.tertiary)
                .padding(.leading, 4)
        }
        .padding(12)
        .background(Color(nsColor: .controlBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(Color.secondary.opacity(0.12), lineWidth: 1)
        )
    }

    private func iconColor(for category: DeviceCategory) -> Color {
        switch category {
        case .appleSilicon: return .blue
        case .usbWiFiDongle: return .purple
        case .ethernet: return .teal
        case .thunderbolt: return .secondary
        case .storageMode: return .orange
        case .generic: return .secondary
        }
    }
}

struct DeviceHeroGraphicView: View {
    let driverName: String
    let category: DeviceCategory

    var body: some View {
        switch category {
        case .usbWiFiDongle:
            USBDongleVectorView()
        case .appleSilicon:
            AppleSiliconChipGraphicView()
        case .ethernet:
            ZStack {
                RoundedRectangle(cornerRadius: 12)
                    .fill(Color(nsColor: .controlBackgroundColor))
                    .overlay(
                        RoundedRectangle(cornerRadius: 12)
                            .stroke(Color.teal.opacity(0.3), lineWidth: 1)
                    )
                VStack(spacing: 8) {
                    Image(systemName: "cable.connector.horizontal")
                        .font(.system(size: 38))
                        .foregroundStyle(Color.teal)
                    Text("Ethernet LAN")
                        .font(.caption.weight(.bold))
                        .foregroundStyle(.primary)
                }
            }
        case .storageMode:
            ZStack {
                RoundedRectangle(cornerRadius: 12)
                    .fill(Color(nsColor: .controlBackgroundColor))
                    .overlay(
                        RoundedRectangle(cornerRadius: 12)
                            .stroke(Color.orange.opacity(0.3), lineWidth: 1)
                    )
                VStack(spacing: 8) {
                    Image(systemName: "externaldrive.badge.wifi")
                        .font(.system(size: 38))
                        .foregroundStyle(Color.orange)
                    Text("ZeroCD Storage")
                        .font(.caption.weight(.bold))
                        .foregroundStyle(.primary)
                }
            }
        case .thunderbolt, .generic:
            ZStack {
                RoundedRectangle(cornerRadius: 12)
                    .fill(Color(nsColor: .controlBackgroundColor))
                VStack(spacing: 8) {
                    Image(systemName: category == .thunderbolt ? "bolt.horizontal.fill" : "cpu")
                        .font(.system(size: 38))
                        .foregroundStyle(.secondary)
                    Text(category == .thunderbolt ? "Thunderbolt" : "Network Adapter")
                        .font(.caption.weight(.bold))
                        .foregroundStyle(.primary)
                }
            }
        }
    }
}

struct AppleSiliconChipGraphicView: View {
    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 12)
                .fill(Color(nsColor: .controlBackgroundColor))
                .overlay(
                    RoundedRectangle(cornerRadius: 12)
                        .stroke(Color.secondary.opacity(0.18), lineWidth: 1)
                )

            VStack(spacing: 8) {
                ZStack {
                    Circle()
                        .fill(Color.blue.opacity(0.12))
                        .frame(width: 44, height: 44)
                    Image(systemName: "laptopcomputer")
                        .font(.system(size: 22))
                        .foregroundStyle(Color.blue)
                }

                Text("Apple Silicon")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(.primary)

                Text("Internal Wi-Fi")
                    .font(.system(size: 9))
                    .foregroundStyle(.secondary)
            }
        }
    }
}
