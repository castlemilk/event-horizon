import SwiftUI
import StarlinkWiFiCore

public struct HardwareMappingView: View {
    let nodes: [HardwareTopologyNode]
    let hotspots: [AccessPoint]
    let interfaceStats: [InterfaceStat]
    let pings: [PingResult]
    let onSelectHotspot: (String) -> Void

    @State private var selectedDeviceForDetail: HardwareTopologyNode?
    @State private var expandedDeviceIDs: Set<String> = []

    public init(
        nodes: [HardwareTopologyNode],
        hotspots: [AccessPoint] = [],
        interfaceStats: [InterfaceStat] = [],
        pings: [PingResult] = [],
        onSelectHotspot: @escaping (String) -> Void = { _ in }
    ) {
        self.nodes = nodes
        self.hotspots = hotspots
        self.interfaceStats = interfaceStats
        self.pings = pings
        self.onSelectHotspot = onSelectHotspot
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            if let selected = selectedDeviceForDetail {
                // Per-Device Dedicated Detail View
                let stat = interfaceStats.first(where: { selected.bsdInterface.contains($0.name) })
                PerDeviceDetailView(
                    node: selected,
                    hotspots: hotspots,
                    stat: stat,
                    pings: pings,
                    onBack: { selectedDeviceForDetail = nil },
                    onSelectHotspot: onSelectHotspot
                )
            } else {
                // Collapsible Device List View
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Connected Devices & Hardware")
                            .font(.title2.weight(.bold))
                        Text("Click any hardware device to inspect full telemetry, run diagnostics, or collapse/expand summary")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                }

                VStack(spacing: 12) {
                    ForEach(nodes) { node in
                        let isExpanded = expandedDeviceIDs.contains(node.id)
                        let stat = interfaceStats.first(where: { node.bsdInterface.contains($0.name) })

                        VStack(spacing: 0) {
                            // Compact Collapsible Header Row
                            HStack(spacing: 12) {
                                Button(action: {
                                    if isExpanded {
                                        expandedDeviceIDs.remove(node.id)
                                    } else {
                                        expandedDeviceIDs.insert(node.id)
                                    }
                                }) {
                                    HStack(spacing: 10) {
                                        Image(systemName: isExpanded ? "chevron.down" : "chevron.right")
                                            .font(.caption.weight(.bold))
                                            .foregroundStyle(.secondary)
                                            .frame(width: 14)

                                        DeviceIconSymbol(name: node.usbDriver)
                                            .font(.title3)
                                            .foregroundStyle(Color.accentColor)

                                        VStack(alignment: .leading, spacing: 2) {
                                            HStack(spacing: 6) {
                                                Text(node.usbDriver)
                                                    .font(.body.weight(.semibold))

                                                Text(node.bsdInterface)
                                                    .font(.caption2.weight(.bold).monospaced())
                                                    .padding(.horizontal, 6)
                                                    .padding(.vertical, 2)
                                                    .background(Color.blue.opacity(0.12))
                                                    .foregroundStyle(.blue)
                                                    .clipShape(RoundedRectangle(cornerRadius: 4))
                                            }

                                            Text("IP: \(node.ipAddress) • \(node.speed)")
                                                .font(.caption2.monospaced())
                                                .foregroundStyle(.secondary)
                                        }
                                    }
                                }
                                .buttonStyle(.plain)

                                Spacer()

                                HStack(spacing: 4) {
                                    Circle()
                                        .fill(node.status.contains("Connected") || node.status.contains("Active") ? Color.green : Color.orange)
                                        .frame(width: 6, height: 6)
                                    Text(node.status)
                                        .font(.caption2.weight(.bold))
                                        .foregroundStyle(node.status.contains("Connected") || node.status.contains("Active") ? .green : .orange)
                                }

                                Button(action: {
                                    selectedDeviceForDetail = node
                                }) {
                                    HStack(spacing: 4) {
                                        Text("Manage")
                                        Image(systemName: "arrow.right")
                                    }
                                    .font(.caption.weight(.medium))
                                }
                                .buttonStyle(.bordered)
                            }
                            .padding(14)

                            // Expanded Detailed Sub-Panel
                            if isExpanded {
                                Divider()
                                DeviceManagementCard(
                                    node: node,
                                    hotspots: hotspots,
                                    stat: stat,
                                    pings: pings,
                                    onSelectHotspot: onSelectHotspot
                                )
                                .padding(12)
                            }
                        }
                        .background(Color(nsColor: .windowBackgroundColor))
                        .clipShape(RoundedRectangle(cornerRadius: 10))
                        .overlay(
                            RoundedRectangle(cornerRadius: 10)
                                .stroke(Color.secondary.opacity(0.12), lineWidth: 1)
                        )
                    }
                }
            }
        }
    }
}

struct DeviceIconSymbol: View {
    let name: String

    var body: some View {
        if name.contains("Apple Silicon") || name.contains("Built-in") || name.contains("Broadcom") {
            Image(systemName: "laptopcomputer")
        } else if name.contains("Wi-Fi") || name.contains("WLAN") {
            Image(systemName: "wifi")
        } else if name.contains("Ethernet") || name.contains("RTL8156") {
            Image(systemName: "cable.connector.horizontal")
        } else {
            Image(systemName: "cpu")
        }
    }
}
