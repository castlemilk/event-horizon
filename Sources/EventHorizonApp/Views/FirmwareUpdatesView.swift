import SwiftUI
import EventHorizonCore

public struct FirmwareUpdatesView: View {
    @Bindable var store: WiFiManagerStore
    let node: HardwareTopologyNode?
    let otherNodes: [HardwareTopologyNode]

    @State private var searchText = ""
    @State private var selectedFamily: String? = nil
    @State private var showingInstallSheet = false
    @State private var selectedVID: UInt16 = 0xa69c
    @State private var selectedPID: UInt16 = 0x8d81
    @State private var selectedDevName: String = "UGREEN AX900 WiFi 6 (AIC8800D80)"

    public init(store: WiFiManagerStore, node: HardwareTopologyNode?, otherNodes: [HardwareTopologyNode]) {
        self.store = store
        self.node = node
        self.otherNodes = otherNodes
    }

    private var filteredChipsets: [ChipsetInfo] {
        store.supportedChipsets.filter { chipset in
            if let family = selectedFamily, !family.isEmpty {
                if chipset.family != family { return false }
            }
            if searchText.isEmpty { return true }
            let q = searchText.lowercased()
            return chipset.family.lowercased().contains(q) ||
                chipset.chipsetName.lowercased().contains(q) ||
                chipset.vendor.lowercased().contains(q) ||
                chipset.standard.lowercased().contains(q) ||
                chipset.supportedIds.contains { $0.productName.lowercased().contains(q) || $0.manufacturer.lowercased().contains(q) }
        }
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            // Header & Search
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Universal Driver & Hardware Matrix")
                        .font(.title2.weight(.bold))
                    Text("Multi-chipset HAL, DriverKit dext status, and global USB Wi-Fi dongle support")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Spacer()

                HStack(spacing: 8) {
                    Image(systemName: "magnifyingglass")
                        .foregroundStyle(.secondary)
                    TextField("Search dongles, chipsets (e.g. TP-Link, Realtek, Wi-Fi 6)...", text: $searchText)
                        .textFieldStyle(.plain)
                        .frame(width: 280)
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .background(Color(nsColor: .controlBackgroundColor))
                .clipShape(RoundedRectangle(cornerRadius: 8))
                .overlay(
                    RoundedRectangle(cornerRadius: 8)
                        .stroke(Color.secondary.opacity(0.15), lineWidth: 1)
                )
            }

            // Supervisor Watchdog & Self-Healing Banner
            SupervisorWatchdogCard(status: store.supervisorStatus)

            // Active Attached Hardware Node
            if let active = node {
                HStack(spacing: 16) {
                    ZStack {
                        Circle()
                            .fill(Color.blue.opacity(0.12))
                            .frame(width: 52, height: 52)
                        Image(systemName: "antenna.radiowaves.left.and.right")
                            .font(.title2)
                            .foregroundStyle(.blue)
                    }

                    VStack(alignment: .leading, spacing: 4) {
                        HStack(spacing: 8) {
                            Text(active.usbDriver)
                                .font(.headline)
                            Text("ACTIVE DONGLE")
                                .font(.system(size: 9, weight: .bold))
                                .padding(.horizontal, 6)
                                .padding(.vertical, 2)
                                .background(Color.green.opacity(0.15))
                                .foregroundStyle(.green)
                                .clipShape(Capsule())
                        }

                        HStack(spacing: 12) {
                            Label(active.status, systemImage: "checkmark.circle.fill")
                                .font(.caption.weight(.medium))
                                .foregroundStyle(.green)
                            Text("•").foregroundStyle(.tertiary)
                            Text("Driver: \(active.driverType)")
                                .font(.caption.monospaced())
                                .foregroundStyle(.secondary)
                            Text("•").foregroundStyle(.tertiary)
                            Text("MAC: \(active.macAddress)")
                                .font(.caption.monospaced())
                                .foregroundStyle(.secondary)
                        }
                    }

                    Spacer()

                    Button(action: {
                        selectedVID = 0xa69c
                        selectedPID = 0x8d81
                        selectedDevName = active.usbDriver
                        showingInstallSheet = true
                    }) {
                        HStack(spacing: 6) {
                            Image(systemName: "bolt.badge.automatic.fill")
                            Text("Re-Flash / Update Driver")
                                .font(.caption.weight(.semibold))
                        }
                        .padding(.horizontal, 12)
                        .padding(.vertical, 6)
                        .background(Color.blue)
                        .foregroundStyle(.white)
                        .clipShape(RoundedRectangle(cornerRadius: 6))
                    }
                    .buttonStyle(.plain)
                }
                .padding(16)
                .background(Color(nsColor: .controlBackgroundColor))
                .clipShape(RoundedRectangle(cornerRadius: 12))
                .overlay(
                    RoundedRectangle(cornerRadius: 12)
                        .stroke(Color.blue.opacity(0.3), lineWidth: 1)
                )
            }

            // Chipset Family Filter Pills
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    FilterPill(title: "All Families (\(store.supportedChipsets.count))", isSelected: selectedFamily == nil) {
                        selectedFamily = nil
                    }

                    ForEach(store.supportedChipsets, id: \.family) { c in
                        FilterPill(title: c.family, isSelected: selectedFamily == c.family) {
                            selectedFamily = c.family
                        }
                    }
                }
            }

            // Chipsets Grid
            LazyVStack(spacing: 14) {
                ForEach(filteredChipsets, id: \.family) { chipset in
                    ChipsetRowCard(
                        chipset: chipset,
                        isCurrentlyActive: node?.usbDriver.contains(chipset.vendor) == true,
                        onInstall: {
                            if let firstDev = chipset.supportedIds.first {
                                selectedVID = firstDev.vid
                                selectedPID = firstDev.pid
                                selectedDevName = firstDev.productName
                            }
                            showingInstallSheet = true
                        }
                    )
                }
            }
        }
        .sheet(isPresented: $showingInstallSheet) {
            DriverInstallWizardSheet(
                store: store,
                vid: selectedVID,
                pid: selectedPID,
                deviceName: selectedDevName,
                onDismiss: {
                    showingInstallSheet = false
                }
            )
        }
    }
}

// MARK: - Supervisor Watchdog Health Card
struct SupervisorWatchdogCard: View {
    let status: SupervisorStatus?

    var body: some View {
        HStack(spacing: 14) {
            ZStack {
                Circle()
                    .fill(Color.green.opacity(0.15))
                    .frame(width: 38, height: 38)
                Image(systemName: "shield.lefthalf.filled.badge.checkmark")
                    .font(.callout)
                    .foregroundStyle(.green)
            }

            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text("Autonomous Runtime Supervisor")
                        .font(.body.weight(.semibold))
                    Text("WATCHDOG ACTIVE")
                        .font(.system(size: 9, weight: .bold))
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(Color.green.opacity(0.15))
                        .foregroundStyle(.green)
                        .clipShape(Capsule())
                }
                Text("Self-healing watchdog monitors USB hardware presence, utun10 virtual route recovery & packet buffers every 3s.")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            VStack(alignment: .trailing, spacing: 2) {
                Text("Self-Heal Events")
                    .font(.system(size: 10))
                    .foregroundStyle(.secondary)
                Text("\(status?.healCount ?? 0) recoveries")
                    .font(.caption.weight(.bold).monospacedDigit())
                    .foregroundStyle(.green)
            }
        }
        .padding(12)
        .background(Color.green.opacity(0.06))
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .overlay(
            RoundedRectangle(cornerRadius: 10)
                .stroke(Color.green.opacity(0.2), lineWidth: 1)
        )
    }
}

// MARK: - Filter Pill
struct FilterPill: View {
    let title: String
    let isSelected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(.caption.weight(isSelected ? .bold : .medium))
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
                .background(isSelected ? Color.blue : Color.secondary.opacity(0.1))
                .foregroundStyle(isSelected ? Color.white : Color.primary)
                .clipShape(Capsule())
        }
        .buttonStyle(.plain)
    }
}

// MARK: - Chipset Row Card
struct ChipsetRowCard: View {
    let chipset: ChipsetInfo
    let isCurrentlyActive: Bool
    let onInstall: () -> Void
    @State private var isExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: 14) {
                Image(systemName: iconForFamily(chipset.family))
                    .font(.title2)
                    .foregroundStyle(isCurrentlyActive ? .green : .blue)
                    .frame(width: 36, height: 36)
                    .background((isCurrentlyActive ? Color.green : Color.blue).opacity(0.1))
                    .clipShape(RoundedRectangle(cornerRadius: 8))

                VStack(alignment: .leading, spacing: 4) {
                    HStack(spacing: 8) {
                        Text(chipset.family)
                            .font(.headline)
                        Text(chipset.standard)
                            .font(.caption2.weight(.bold))
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Color.secondary.opacity(0.12))
                            .clipShape(RoundedRectangle(cornerRadius: 4))

                        Spacer()

                        Button(action: onInstall) {
                            HStack(spacing: 4) {
                                Image(systemName: "bolt.fill")
                                Text("Install / Flash")
                                    .font(.caption.weight(.semibold))
                            }
                            .padding(.horizontal, 10)
                            .padding(.vertical, 4)
                            .background(Color.blue)
                            .foregroundStyle(.white)
                            .clipShape(RoundedRectangle(cornerRadius: 6))
                        }
                        .buttonStyle(.plain)
                    }

                    Text(chipset.description)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(isExpanded ? nil : 2)

                    // Capabilities Tags
                    FlowLayout(spacing: 6) {
                        ForEach(chipset.capabilities, id: \.self) { cap in
                            Text(cap)
                                .font(.system(size: 10, weight: .semibold))
                                .padding(.horizontal, 6)
                                .padding(.vertical, 2)
                                .background(Color.secondary.opacity(0.08))
                                .clipShape(RoundedRectangle(cornerRadius: 4))
                        }
                    }
                    .padding(.top, 2)
                }
            }

            Divider()

            // Supported Commercial Dongle Models Accordion
            DisclosureGroup(
                isExpanded: $isExpanded,
                content: {
                    VStack(alignment: .leading, spacing: 6) {
                        Text("KNOWN COMMERCIAL HARDWARE PRODUCTS (\(chipset.supportedIds.count)):")
                            .font(.system(size: 10, weight: .bold))
                            .foregroundStyle(.secondary)
                            .padding(.top, 6)

                        ForEach(chipset.supportedIds, id: \.id) { dev in
                            HStack(spacing: 10) {
                                Image(systemName: dev.isStorage ? "opticaldisc.fill" : "wifi")
                                    .font(.caption2)
                                    .foregroundStyle(dev.isStorage ? .orange : .blue)

                                Text(dev.productName)
                                    .font(.caption.weight(.medium))

                                Spacer()

                                Text("\(dev.vendorHex):\(dev.productHex)")
                                    .font(.caption2.monospaced())
                                    .foregroundStyle(.secondary)

                                Text(dev.manufacturer)
                                    .font(.system(size: 10))
                                    .padding(.horizontal, 5)
                                    .padding(.vertical, 1)
                                    .background(Color.secondary.opacity(0.1))
                                    .clipShape(RoundedRectangle(cornerRadius: 3))
                            }
                            .padding(.vertical, 3)
                        }
                    }
                },
                label: {
                    HStack {
                        Text("Commercial Dongle Coverage (\(chipset.supportedIds.count) models verified)")
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.blue)
                        Spacer()
                    }
                }
            )
        }
        .padding(14)
        .background(Color(nsColor: .controlBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .overlay(
            RoundedRectangle(cornerRadius: 10)
                .stroke(isCurrentlyActive ? Color.green.opacity(0.3) : Color.secondary.opacity(0.12), lineWidth: 1)
        )
    }

    private func iconForFamily(_ family: String) -> String {
        if family.contains("AIC8800") || family.contains("AicSemi") { return "bolt.shield.fill" }
        if family.contains("Wi-Fi 6") || family.contains("802.11ax") { return "wifi.circle.fill" }
        if family.contains("Realtek") { return "cpu.fill" }
        if family.contains("MediaTek") { return "antenna.radiowaves.left.and.right.circle.fill" }
        if family.contains("Atheros") { return "wave.3.left.circle.fill" }
        return "wifi"
    }
}

// MARK: - Flow Layout for Tags
struct FlowLayout: Layout {
    var spacing: CGFloat = 6

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? 0
        var height: CGFloat = 0
        var x: CGFloat = 0
        var y: CGFloat = 0
        var maxHeightInRow: CGFloat = 0

        for subview in subviews {
            let size = subview.sizeThatFits(ProposedViewSize(width: nil, height: nil))
            if x + size.width > width && x > 0 {
                x = 0
                y += maxHeightInRow + spacing
                maxHeightInRow = 0
            }
            x += size.width + spacing
            maxHeightInRow = max(maxHeightInRow, size.height)
        }
        height = y + maxHeightInRow
        return CGSize(width: width, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX
        var y = bounds.minY
        var maxHeightInRow: CGFloat = 0

        for subview in subviews {
            let size = subview.sizeThatFits(ProposedViewSize(width: nil, height: nil))
            if x + size.width > bounds.maxX && x > bounds.minX {
                x = bounds.minX
                y += maxHeightInRow + spacing
                maxHeightInRow = 0
            }
            subview.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(size))
            x += size.width + spacing
            maxHeightInRow = max(maxHeightInRow, size.height)
        }
    }
}
