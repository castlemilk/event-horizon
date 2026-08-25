import SwiftUI
import EventHorizonCore

public struct HardwareMappingView: View {
    let nodes: [HardwareTopologyNode]
    let hotspots: [AccessPoint]
    let interfaceStats: [InterfaceStat]
    let pings: [PingResult]
    let onSelectHotspot: (String) -> Void

    @Binding var selectedDeviceForDetail: HardwareTopologyNode?
    @State private var expandedDeviceIDs: Set<String> = []

    public init(
        nodes: [HardwareTopologyNode],
        hotspots: [AccessPoint] = [],
        interfaceStats: [InterfaceStat] = [],
        pings: [PingResult] = [],
        selectedDevice: Binding<HardwareTopologyNode?> = .constant(nil),
        onSelectHotspot: @escaping (String) -> Void = { _ in }
    ) {
        self.nodes = nodes
        self.hotspots = hotspots
        self.interfaceStats = interfaceStats
        self.pings = pings
        self._selectedDeviceForDetail = selectedDevice
        self.onSelectHotspot = onSelectHotspot
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 14) {
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
                VStack(spacing: 10) {
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

                                        Image(systemName: node.category.systemIconName)
                                            .font(.title3)
                                            .foregroundStyle(categoryColor(for: node.category))
                                            .frame(width: 24)

                                        VStack(alignment: .leading, spacing: 2) {
                                            HStack(spacing: 6) {
                                                Text(node.usbDriver)
                                                    .font(.body.weight(.semibold))

                                                if !node.bsdInterface.isEmpty {
                                                    Text(node.bsdInterface)
                                                        .font(.caption2.weight(.bold).monospaced())
                                                        .padding(.horizontal, 5)
                                                        .padding(.vertical, 1)
                                                        .background(Color.secondary.opacity(0.12))
                                                        .foregroundStyle(.primary)
                                                        .clipShape(RoundedRectangle(cornerRadius: 3))
                                                }

                                                Text("[\(node.category.shortLabel)]")
                                                    .font(.caption2.weight(.medium))
                                                    .foregroundStyle(.secondary)
                                            }

                                            HStack(spacing: 6) {
                                                if !node.ipAddress.isEmpty {
                                                    Text("IP: \(node.ipAddress)")
                                                        .font(.caption2.monospaced())
                                                        .foregroundStyle(.secondary)
                                                }
                                                if !node.speed.isEmpty {
                                                    Text("• \(node.speed)")
                                                        .font(.caption2)
                                                        .foregroundStyle(.tertiary)
                                                }
                                                if !node.networkTarget.isEmpty {
                                                    HStack(spacing: 3) {
                                                        Image(systemName: "wifi")
                                                            .font(.system(size: 8))
                                                            .foregroundStyle(.blue)
                                                        Text(node.networkTarget)
                                                            .font(.caption2.weight(.medium))
                                                            .foregroundStyle(.secondary)
                                                    }
                                                }
                                            }
                                        }
                                    }
                                }
                                .buttonStyle(.plain)

                                Spacer()

                                HStack(spacing: 4) {
                                    Circle()
                                        .fill(node.isDefaultRoute ? Color.green : (node.isStorageMode ? Color.orange : Color.secondary))
                                        .frame(width: 6, height: 6)
                                    Text(node.routeBadge)
                                        .font(.caption2.weight(.bold))
                                        .foregroundStyle(node.isDefaultRoute ? .green : (node.isStorageMode ? .orange : .secondary))
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
                                .controlSize(.small)
                            }
                            .padding(12)

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

    private func categoryColor(for category: DeviceCategory) -> Color {
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
