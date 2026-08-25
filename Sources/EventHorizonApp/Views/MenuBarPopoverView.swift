import SwiftUI
import EventHorizonCore

enum PopoverTab: String, CaseIterable, Identifiable {
    case active = "Active"
    case scan = "Join Wi-Fi"
    case devices = "Hardware"

    var id: String { rawValue }

    var iconName: String {
        switch self {
        case .active: return "wifi"
        case .scan: return "antenna.radiowaves.left.and.right"
        case .devices: return "cpu"
        }
    }
}

public struct MenuBarPopoverView: View {
    @Bindable var store: WiFiManagerStore
    @Environment(\.openWindow) private var openWindow
    @State private var selectedTab: PopoverTab = .active
    @State private var expandingSSID: String?
    @State private var passphrase = ""
    @FocusState private var isPassphraseFocused: Bool

    public init(store: WiFiManagerStore) {
        self.store = store
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            header
            Divider()

            // MARK: - Tab Segmented Control with Count Bubble
            HStack(spacing: 3) {
                tabButton(title: "Active", badgeCount: store.activeConnectedNodes.count, tab: .active)
                tabButton(title: "Join Wi-Fi", badgeCount: nil, tab: .scan)
                tabButton(title: "Hardware", badgeCount: nil, tab: .devices)
            }
            .padding(3)
            .background(Color.secondary.opacity(0.12))
            .clipShape(RoundedRectangle(cornerRadius: 8))

            // MARK: - Tab Content Views
            Group {
                switch selectedTab {
                case .active:
                    activeConnectionsTab
                case .scan:
                    joinWiFiTab
                case .devices:
                    hardwareDevicesTab
                }
            }
            .frame(minHeight: 220)

            Divider()
            footer
        }
        .padding(12)
        .frame(width: 340)
        .onAppear {
            Task {
                await store.refreshData()
            }
        }
    }

    // MARK: - Header

    private var header: some View {
        HStack(spacing: 6) {
            Image(systemName: "circle.circle.fill")
                .font(.system(size: 13, weight: .bold))
                .foregroundStyle(Color.accentColor)

            Text("Event Horizon")
                .font(.system(size: 13, weight: .semibold))

            Spacer()

            if store.isConnecting {
                ProgressView()
                    .controlSize(.small)
            }

            HStack(spacing: 4) {
                Circle()
                    .fill(store.isDaemonConnected ? Color.green : Color.red)
                    .frame(width: 6, height: 6)

                Text(store.isDaemonConnected ? "Ready" : "Offline")
                    .font(.caption2.weight(.medium))
                    .foregroundStyle(.secondary)
            }
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(Color.secondary.opacity(0.08))
            .clipShape(Capsule())
        }
    }

    // MARK: - Tab 1: Active Connections

    private var activeConnectionsTab: some View {
        VStack(alignment: .leading, spacing: 8) {
            if store.activeConnectedNodes.isEmpty {
                VStack(spacing: 12) {
                    Spacer()
                    Image(systemName: "wifi.slash")
                        .font(.system(size: 28))
                        .foregroundStyle(.secondary)
                    Text("No Active Wi-Fi Connections")
                        .font(.headline)
                        .foregroundStyle(.secondary)
                    Text("Select a Wi-Fi network from the Join tab to connect your built-in radio or USB dongle.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                        .padding(.horizontal, 16)

                    Button("Scan & Join Wi-Fi") {
                        selectedTab = .scan
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                    Spacer()
                }
                .frame(maxWidth: .infinity)
            } else {
                ScrollView {
                    VStack(alignment: .leading, spacing: 8) {
                        ForEach(store.activeConnectedNodes, id: \.bsdInterface) { node in
                            ConnectedDeviceCard(store: store, node: node)
                        }

                        // Summary Telemetry Pill
                        if let firstStat = store.interfaceStats.first {
                            HStack {
                                Label("Total Traffic", systemImage: "network")
                                    .font(.caption2.weight(.semibold))
                                    .foregroundStyle(.secondary)
                                Spacer()
                                Text("↓ \(formatSpeed(firstStat.rxRateKBps))  ·  ↑ \(formatSpeed(firstStat.txRateKBps))")
                                    .font(.caption2.weight(.bold).monospacedDigit())
                                    .foregroundStyle(.primary)
                            }
                            .padding(.horizontal, 8)
                            .padding(.vertical, 5)
                            .background(Color.secondary.opacity(0.05))
                            .clipShape(RoundedRectangle(cornerRadius: 6))
                        }
                    }
                }
            }
        }
    }

    // MARK: - Tab 2: Join / Scan Wi-Fi

    private var joinWiFiTab: some View {
        VStack(alignment: .leading, spacing: 8) {
            // Target Adapter Dropdown
            activeDeviceSelector

            HStack {
                Text("AVAILABLE NETWORKS")
                    .font(.system(size: 9, weight: .bold))
                    .foregroundStyle(.secondary)

                Spacer()

                Button(action: {
                    Task { await store.refreshData() }
                }) {
                    HStack(spacing: 3) {
                        Image(systemName: "arrow.clockwise")
                        Text("Rescan")
                    }
                    .font(.system(size: 9))
                }
                .buttonStyle(.plain)
                .foregroundStyle(.blue)
            }

            if !store.isDaemonConnected {
                Label("USB Daemon offline — click Open Dashboard", systemImage: "exclamationmark.triangle.fill")
                    .font(.caption2)
                    .foregroundStyle(.orange)
                    .padding(.vertical, 6)
            } else if connectableHotspots.isEmpty {
                VStack(spacing: 6) {
                    ProgressView()
                        .scaleEffect(0.7)
                    Text("Scanning 802.11 channels...")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ScrollView {
                    VStack(spacing: 3) {
                        ForEach(connectableHotspots) { ap in
                            networkRow(ap)
                        }
                    }
                }
            }
        }
    }

    // MARK: - Tab 3: Hardware Inspector & Driver Manager

    private var hardwareDevicesTab: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 8) {
                // Driver Service Card
                VStack(alignment: .leading, spacing: 5) {
                    HStack {
                        Image(systemName: store.isDaemonConnected ? "bolt.shield.fill" : "exclamationmark.triangle.fill")
                            .foregroundStyle(store.isDaemonConnected ? .green : .orange)
                        Text(store.isDaemonConnected ? "Driver Agent Active (Port 8990)" : "Driver Agent Offline")
                            .font(.system(size: 10, weight: .bold))
                        Spacer()
                        if store.isDaemonConnected {
                            Button("Restart") {
                                Task { await store.restartDaemonService() }
                            }
                            .buttonStyle(.bordered)
                            .controlSize(.mini)
                            .font(.system(size: 9))
                        } else {
                            Button("Start Agent") {
                                Task { await store.installLaunchDaemonService() }
                            }
                            .buttonStyle(.borderedProminent)
                            .controlSize(.mini)
                            .font(.system(size: 9, weight: .semibold))
                        }
                    }

                    Text("Embedded usbwifi daemon manages USB claiming, firmware uploads, and utun interfaces.")
                        .font(.system(size: 8))
                        .foregroundStyle(.secondary)
                }
                .padding(8)
                .background(store.isDaemonConnected ? Color.green.opacity(0.06) : Color.orange.opacity(0.08))
                .clipShape(RoundedRectangle(cornerRadius: 6))

                Text("DETECTED HARDWARE & ADAPTERS")
                    .font(.system(size: 9, weight: .bold))
                    .foregroundStyle(.secondary)
                    .padding(.top, 2)

                ForEach(store.topologyNodes) { node in
                    HStack(spacing: 8) {
                        Image(systemName: iconForNode(node))
                            .font(.system(size: 14, weight: .semibold))
                            .foregroundStyle(iconColor(for: node))
                            .frame(width: 22)

                        VStack(alignment: .leading, spacing: 2) {
                            Text(node.usbDriver)
                                .font(.system(size: 11, weight: .semibold))
                                .lineLimit(1)

                            HStack(spacing: 4) {
                                if !node.bsdInterface.isEmpty {
                                    Text(node.bsdInterface)
                                        .font(.system(size: 9, weight: .bold).monospaced())
                                        .padding(.horizontal, 4)
                                        .padding(.vertical, 1)
                                        .background(Color.secondary.opacity(0.1))
                                        .clipShape(RoundedRectangle(cornerRadius: 3))
                                }

                                if !node.macAddress.isEmpty {
                                    Text(node.macAddress)
                                        .font(.system(size: 8).monospaced())
                                        .foregroundStyle(.secondary)
                                }
                            }
                        }

                        Spacer()

                        if node.isStorageMode {
                            Button("Switch to Wi-Fi") {
                                Task { await store.modeSwitchDongle() }
                            }
                            .buttonStyle(.borderedProminent)
                            .controlSize(.mini)
                            .font(.system(size: 9, weight: .semibold))
                        } else if node.category == .usbWiFiDongle {
                            Button("Configure") {
                                Task {
                                    await store.startDriverInstallation(vid: 0xa69c, pid: 0x8d81)
                                }
                            }
                            .buttonStyle(.bordered)
                            .controlSize(.mini)
                            .font(.system(size: 9, weight: .medium))
                        } else {
                            Text(node.status)
                                .font(.system(size: 9, weight: .medium))
                                .foregroundStyle(node.isDefaultRoute || !node.networkTarget.isEmpty ? Color.green : Color.secondary)
                                .padding(.horizontal, 6)
                                .padding(.vertical, 2)
                                .background(node.isDefaultRoute || !node.networkTarget.isEmpty ? Color.green.opacity(0.1) : Color.secondary.opacity(0.08))
                                .clipShape(Capsule())
                        }
                    }
                    .padding(8)
                    .background(Color(nsColor: .controlBackgroundColor))
                    .clipShape(RoundedRectangle(cornerRadius: 6))
                    .overlay(
                        RoundedRectangle(cornerRadius: 6)
                            .stroke(Color.secondary.opacity(0.1), lineWidth: 1)
                    )
                }
            }
        }
    }

    // MARK: - Target Adapter Dropdown

    private var activeDeviceSelector: some View {
        Menu {
            ForEach(store.quickSelectDevices) { node in
                if node.isStorageMode {
                    Button {
                        Task { await store.modeSwitchDongle() }
                    } label: {
                        Label("\(node.usbDriver) [Storage Mode -> Switch to Wi-Fi]", systemImage: "arrow.triangle.2.circlepath")
                    }
                } else if !node.bsdInterface.isEmpty {
                    Button {
                        store.selectDeviceInterface(node.bsdInterface)
                    } label: {
                        HStack {
                            Text("[\(node.category.shortLabel)] \(node.usbDriver) (\(node.bsdInterface))")
                            if node.isDefaultRoute {
                                Text("• Default Route")
                            }
                        }
                    }
                } else {
                    Button {
                        store.selectDongle(node)
                    } label: {
                        Text("[\(node.category.shortLabel)] \(node.usbDriver) • \(node.status)")
                    }
                }
            }
        } label: {
            HStack(spacing: 8) {
                Image(systemName: selectedNode?.category.systemIconName ?? "cpu")
                    .font(.system(size: 13, weight: .medium))
                    .foregroundStyle(iconColor(for: selectedNode))
                    .frame(width: 20)

                VStack(alignment: .leading, spacing: 1) {
                    HStack(spacing: 4) {
                        Text("Target:")
                            .font(.system(size: 9))
                            .foregroundStyle(.secondary)
                        Text(selectedDeviceTitle)
                            .font(.system(size: 11, weight: .semibold))
                            .foregroundStyle(.primary)
                            .lineLimit(1)
                    }
                }

                Spacer()

                Image(systemName: "chevron.up.chevron.down")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 5)
            .background(Color(nsColor: .controlBackgroundColor))
            .clipShape(RoundedRectangle(cornerRadius: 6))
            .overlay(
                RoundedRectangle(cornerRadius: 6)
                    .stroke(Color.secondary.opacity(0.15), lineWidth: 1)
            )
        }
        .menuStyle(.borderlessButton)
    }

    private func networkRow(_ ap: AccessPoint) -> some View {
        let isExpanded = expandingSSID == ap.ssid
        let isConnected = store.activeConnectedNodes.contains { $0.networkTarget == ap.ssid }

        return VStack(spacing: 4) {
            HStack(spacing: 8) {
                signalMeter(rssi: ap.rssi)

                VStack(alignment: .leading, spacing: 1) {
                    HStack(spacing: 4) {
                        Text(ap.ssid)
                            .font(.system(size: 11, weight: isConnected ? .bold : .medium))
                            .lineLimit(1)

                        if !ap.security.isEmpty && ap.security != "Open" {
                            Image(systemName: "lock.fill")
                                .font(.system(size: 8))
                                .foregroundStyle(.secondary)
                        }
                    }

                    HStack(spacing: 4) {
                        Text(ap.channel > 14 ? "5 GHz" : "2.4 GHz")
                            .font(.system(size: 8))
                            .foregroundStyle(.secondary)
                        Text("•")
                            .font(.system(size: 8))
                            .foregroundStyle(.tertiary)
                        Text("\(ap.rssi) dBm")
                            .font(.system(size: 8).monospaced())
                            .foregroundStyle(.secondary)
                    }
                }

                Spacer()

                if isConnected {
                    Image(systemName: "checkmark.circle.fill")
                        .font(.system(size: 12))
                        .foregroundStyle(.green)
                } else if isExpanded {
                    Button("Cancel") {
                        withAnimation(.easeInOut(duration: 0.18)) {
                            expandingSSID = nil
                            passphrase = ""
                        }
                    }
                    .buttonStyle(.plain)
                    .font(.system(size: 10))
                    .foregroundStyle(.secondary)
                } else {
                    Button("Connect") {
                        withAnimation(.easeInOut(duration: 0.18)) {
                            expandingSSID = ap.ssid
                            passphrase = ""
                            isPassphraseFocused = true
                        }
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.mini)
                    .font(.system(size: 10, weight: .medium))
                }
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(
                RoundedRectangle(cornerRadius: 5)
                    .fill(isConnected ? Color.green.opacity(0.08) : (isExpanded ? Color.accentColor.opacity(0.08) : Color.secondary.opacity(0.04)))
            )

            // Inline Passphrase Expansion
            if isExpanded && !isConnected {
                HStack(spacing: 6) {
                    SecureField("Wi-Fi Password", text: $passphrase)
                        .textFieldStyle(.roundedBorder)
                        .controlSize(.small)
                        .focused($isPassphraseFocused)
                        .onSubmit {
                            Task {
                                let target = ap.ssid
                                let pass = passphrase
                                expandingSSID = nil
                                passphrase = ""
                                await store.connect(to: target, passphrase: pass)
                            }
                        }

                    Button("Join") {
                        Task {
                            let target = ap.ssid
                            let pass = passphrase
                            expandingSSID = nil
                            passphrase = ""
                            await store.connect(to: target, passphrase: pass)
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                    .disabled(passphrase.isEmpty)
                }
                .padding(.horizontal, 8)
                .padding(.bottom, 4)
                .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
    }

    // MARK: - Footer

    private var footer: some View {
        HStack {
            Button(action: {
                openWindow(id: "dashboard")
                NSApp.activate(ignoringOtherApps: true)
            }) {
                HStack(spacing: 4) {
                    Image(systemName: "macwindow")
                    Text("Dashboard")
                }
                .font(.system(size: 11, weight: .medium))
            }
            .buttonStyle(.bordered)
            .controlSize(.small)

            Button(action: {
                Task { await store.refreshData() }
            }) {
                Image(systemName: "arrow.clockwise")
                    .font(.system(size: 10))
            }
            .buttonStyle(.bordered)
            .controlSize(.small)

            Spacer()

            Button("Quit") {
                NSApplication.shared.terminate(nil)
            }
            .buttonStyle(.plain)
            .font(.system(size: 11))
            .foregroundStyle(.secondary)
        }
    }

    // MARK: - Helpers

    private var selectedNode: HardwareTopologyNode? {
        if let did = store.selectedDongleId,
           let n = store.quickSelectDevices.first(where: { HardwareTopologyNode.dongleId($0) == did }) {
            return n
        }
        return store.quickSelectDevices.first { $0.bsdInterface == store.selectedInterface }
            ?? store.quickSelectDevices.first
    }

    private var selectedDeviceTitle: String {
        guard let node = selectedNode else { return "No device detected" }
        return node.bsdInterface.isEmpty ? node.usbDriver : "\(node.usbDriver) (\(node.bsdInterface))"
    }

    private func tabButton(title: String, badgeCount: Int?, tab: PopoverTab) -> some View {
        let isSelected = selectedTab == tab
        return Button(action: {
            withAnimation(.easeInOut(duration: 0.15)) {
                selectedTab = tab
            }
        }) {
            HStack(spacing: 5) {
                Text(title)
                    .font(.system(size: 11, weight: isSelected ? .bold : .medium))
                    .foregroundStyle(isSelected ? .primary : .secondary)

                if let count = badgeCount, count > 0 {
                    Text("\(count)")
                        .font(.system(size: 9, weight: .bold).monospacedDigit())
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(isSelected ? Color.green : Color.secondary.opacity(0.2))
                        .foregroundStyle(isSelected ? Color.white : Color.secondary)
                        .clipShape(Capsule())
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 4)
            .background(
                RoundedRectangle(cornerRadius: 6)
                    .fill(isSelected ? Color(nsColor: .controlBackgroundColor) : Color.clear)
                    .shadow(color: isSelected ? Color.black.opacity(0.12) : Color.clear, radius: 1, x: 0, y: 1)
            )
        }
        .buttonStyle(.plain)
    }

    private var connectableHotspots: [AccessPoint] {
        let connectedSSIDs = Set(store.activeConnectedNodes.map(\.networkTarget))
        return store.hotspots.filter {
            !$0.ssid.isEmpty
                && $0.ssid != "<hidden>"
                && $0.ssid != "<redacted>"
                && $0.rssi > -85
                && !connectedSSIDs.contains($0.ssid)
        }
    }

    private func signalMeter(rssi: Int8) -> some View {
        HStack(alignment: .bottom, spacing: 1.5) {
            Capsule()
                .fill(rssi > -85 ? Color.green : Color.secondary.opacity(0.25))
                .frame(width: 2.5, height: 4)
            Capsule()
                .fill(rssi > -70 ? Color.green : Color.secondary.opacity(0.25))
                .frame(width: 2.5, height: 7)
            Capsule()
                .fill(rssi > -55 ? Color.green : Color.secondary.opacity(0.25))
                .frame(width: 2.5, height: 10)
        }
        .frame(width: 12)
    }

    private func iconForNode(_ node: HardwareTopologyNode) -> String {
        if node.bsdInterface == "en0" || node.category == .appleSilicon { return "laptopcomputer" }
        if node.category == .usbWiFiDongle || node.bsdInterface.contains("utun") { return "antenna.radiowaves.left.and.right" }
        return node.category.systemIconName
    }

    private func iconColor(for node: HardwareTopologyNode?) -> Color {
        guard let node else { return .secondary }
        if node.isDefaultRoute { return .blue }
        if node.category == .usbWiFiDongle || node.bsdInterface.contains("utun") { return .purple }
        return .teal
    }

    private func formatSpeed(_ kbps: Double) -> String {
        if kbps >= 1024 {
            return String(format: "%.1f MB/s", kbps / 1024.0)
        }
        return String(format: "%d KB/s", Int(kbps))
    }
}

// MARK: - Connected Device Card
struct ConnectedDeviceCard: View {
    @Bindable var store: WiFiManagerStore
    let node: HardwareTopologyNode

    var isTargeted: Bool {
        store.selectedInterface == node.bsdInterface
    }

    var matchingHotspot: AccessPoint? {
        store.hotspots.first(where: { $0.ssid == node.networkTarget })
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            // Device Header
            HStack(spacing: 6) {
                Image(systemName: iconForNode(node))
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(iconColor(for: node))

                Text(cleanDeviceName(node.usbDriver))
                    .font(.system(size: 11, weight: .bold))
                    .lineLimit(1)

                Spacer()

                if !node.bsdInterface.isEmpty {
                    Text(node.bsdInterface)
                        .font(.system(size: 9, weight: .bold).monospaced())
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(Color.secondary.opacity(0.12))
                        .clipShape(RoundedRectangle(cornerRadius: 3))
                }

                Text(node.isDefaultRoute ? "DEFAULT ROUTE" : "ISOLATED")
                    .font(.system(size: 8, weight: .bold))
                    .padding(.horizontal, 5)
                    .padding(.vertical, 1)
                    .background(node.isDefaultRoute ? Color.blue.opacity(0.15) : Color.purple.opacity(0.15))
                    .foregroundStyle(node.isDefaultRoute ? Color.blue : Color.purple)
                    .clipShape(Capsule())
            }

            // Connection Target & Signal
            HStack(spacing: 8) {
                Image(systemName: "wifi")
                    .font(.system(size: 13, weight: .bold))
                    .foregroundStyle(.green)

                VStack(alignment: .leading, spacing: 1) {
                    Text(node.networkTarget)
                        .font(.system(size: 12, weight: .bold))
                        .foregroundStyle(.primary)

                    HStack(spacing: 4) {
                        if !node.ipAddress.isEmpty {
                            Text(node.ipAddress)
                                .font(.system(size: 9).monospaced())
                                .foregroundStyle(.secondary)
                        }
                        if let hp = matchingHotspot, hp.channel > 0 {
                            Text("•").foregroundStyle(.tertiary)
                            Text(hp.channel > 14 ? "5 GHz" : "2.4 GHz")
                                .font(.system(size: 9))
                                .foregroundStyle(.secondary)
                            Text("•").foregroundStyle(.tertiary)
                            Text("\(hp.rssi) dBm")
                                .font(.system(size: 9).monospaced())
                                .foregroundStyle(.secondary)
                        }
                    }
                }

                Spacer()

                if !isTargeted && !node.bsdInterface.isEmpty {
                    Button("Select") {
                        store.selectDeviceInterface(node.bsdInterface)
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.mini)
                    .font(.system(size: 9, weight: .medium))
                }

                Button("Disconnect") {
                    Task { await store.disconnect() }
                }
                .buttonStyle(.bordered)
                .controlSize(.mini)
                .font(.system(size: 9))
            }

            // Live Throughput / Ping if targeted
            if isTargeted, let stat = store.interfaceStats.first(where: { $0.name == node.bsdInterface }) ?? store.interfaceStats.first {
                HStack(spacing: 10) {
                    HStack(spacing: 3) {
                        Image(systemName: "arrow.down")
                            .font(.system(size: 8, weight: .bold))
                            .foregroundStyle(.blue)
                        Text(formatSpeed(stat.rxRateKBps))
                            .font(.system(size: 9, weight: .semibold).monospacedDigit())
                    }

                    HStack(spacing: 3) {
                        Image(systemName: "arrow.up")
                            .font(.system(size: 8, weight: .bold))
                            .foregroundStyle(.teal)
                        Text(formatSpeed(stat.txRateKBps))
                            .font(.system(size: 9, weight: .semibold).monospacedDigit())
                    }

                    Spacer()

                    if let rtt = store.pingResults.first?.rttMs, rtt > 0 {
                        Text("RTT \(rtt) ms")
                            .font(.system(size: 8).monospacedDigit())
                            .foregroundStyle(.secondary)
                    }
                }
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(Color.secondary.opacity(0.06))
                .clipShape(RoundedRectangle(cornerRadius: 4))
            }
        }
        .padding(8)
        .background(Color.green.opacity(0.08))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(isTargeted ? Color.green.opacity(0.4) : Color.green.opacity(0.2), lineWidth: isTargeted ? 1.5 : 1)
        )
    }

    private func cleanDeviceName(_ raw: String) -> String {
        if raw.contains("Built-in") { return "Apple Built-in Wi-Fi" }
        if raw.contains("UGREEN") { return "UGREEN AX900 Wi-Fi 6" }
        return raw
    }

    private func iconForNode(_ node: HardwareTopologyNode) -> String {
        if node.bsdInterface == "en0" || node.category == .appleSilicon { return "laptopcomputer" }
        if node.category == .usbWiFiDongle || node.bsdInterface.contains("utun") { return "antenna.radiowaves.left.and.right" }
        return node.category.systemIconName
    }

    private func iconColor(for node: HardwareTopologyNode) -> Color {
        if node.isDefaultRoute { return .blue }
        if node.category == .usbWiFiDongle || node.bsdInterface.contains("utun") { return .purple }
        return .teal
    }

    private func formatSpeed(_ kbps: Double) -> String {
        if kbps >= 1024 {
            return String(format: "%.1f MB/s", kbps / 1024.0)
        }
        return String(format: "%d KB/s", Int(kbps))
    }
}
