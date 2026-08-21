import SwiftUI
import EventHorizonCore

public struct MenuBarPopoverView: View {
    @Bindable var store: WiFiManagerStore
    @Environment(\.openWindow) private var openWindow
    @State private var pendingSSID: String?
    @State private var passphrase = ""
    @State private var isShowingPassphrasePrompt = false

    public init(store: WiFiManagerStore) {
        self.store = store
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            header
            Divider()
            stepLabel("1 · DEVICE", systemImage: "cpu.fill")
            devicePicker
            Divider()
            stepLabel("2 · NETWORK", systemImage: "wifi")
            networkList
            Divider()
            footer
        }
        .padding(12)
        .frame(width: 296)
        .alert("Connect to \"\(pendingSSID ?? "")\"", isPresented: $isShowingPassphrasePrompt) {
            SecureField("Wi-Fi passphrase", text: $passphrase)
            Button("Connect") {
                guard let ssid = pendingSSID, !ssid.isEmpty else { return }
                Task {
                    await store.connect(to: ssid, passphrase: passphrase)
                }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Enter the passphrase for this network.")
        }
    }

    // MARK: - Sections

    private var header: some View {
        HStack(spacing: 6) {
            Text("Event Horizon")
                .font(.system(size: 12, weight: .semibold))
            Spacer()
            if store.isConnecting {
                ProgressView()
                    .controlSize(.small)
            }
            Circle()
                .fill(store.isDaemonConnected ? Color.green : Color.red)
                .frame(width: 7, height: 7)
        }
    }

    private var devicePicker: some View {
        Menu {
            ForEach(store.quickSelectDevices) { node in
                if node.bsdInterface.isEmpty {
                    if node.status.localizedCaseInsensitiveContains("storage") {
                        Button {
                            Task { await store.modeSwitchDongle() }
                        } label: {
                            VStack(alignment: .leading, spacing: 1) {
                                HStack(spacing: 4) {
                                    Text(node.usbDriver)
                                    Image(systemName: "arrow.triangle.2.circlepath")
                                        .font(.caption2)
                                        .foregroundStyle(.secondary)
                                }
                                Text(node.status)
                                    .font(.caption2)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    } else {
                        Button {
                            store.selectDongle(node)
                        } label: {
                            VStack(alignment: .leading, spacing: 1) {
                                Text(node.usbDriver)
                                Text(node.status)
                                    .font(.caption2)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                } else {
                    Button {
                        store.selectDeviceInterface(node.bsdInterface)
                    } label: {
                        Text("\(node.usbDriver) (\(node.bsdInterface))")
                    }
                }
            }
        } label: {
            HStack(spacing: 8) {
                Image(systemName: selectedDeviceIcon)
                    .font(.body)
                    .foregroundStyle(.blue)
                VStack(alignment: .leading, spacing: 1) {
                    Text(selectedDeviceTitle)
                        .font(.system(size: 12, weight: .medium))
                        .foregroundStyle(.primary)
                        .lineLimit(1)
                    Text(selectedDeviceSubtitle)
                        .font(.system(size: 10))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer()
                Image(systemName: "chevron.up.chevron.down")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 7)
            .background(
                RoundedRectangle(cornerRadius: 7)
                    .fill(Color.secondary.opacity(0.07))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 7)
                    .stroke(Color.secondary.opacity(0.15), lineWidth: 1)
            )
        }
        .menuStyle(.borderlessButton)
    }

    private var networkList: some View {
        VStack(spacing: 4) {
            if !store.isDaemonConnected {
                Label("Daemon offline — restart Event Horizon", systemImage: "exclamationmark.triangle.fill")
                    .font(.caption2)
                    .foregroundStyle(.red)
                    .padding(.vertical, 6)
            } else if connectableHotspots.isEmpty {
                Text("No networks in range")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .padding(.vertical, 8)
            } else {
                ForEach(connectableHotspots.prefix(5)) { ap in
                    networkRow(ap)
                }
            }
        }
    }

    private func networkRow(_ ap: AccessPoint) -> some View {
        HStack(spacing: 8) {
            Image(systemName: signalIcon(for: ap.rssi))
                .font(.caption)
                .foregroundStyle(ap.isSelected ? Color.green : Color.primary.opacity(0.8))

            VStack(alignment: .leading, spacing: 1) {
                Text(ap.ssid)
                    .font(.caption.weight(ap.isSelected ? .semibold : .regular))
                    .lineLimit(1)
                HStack(spacing: 4) {
                    Text(ap.security.isEmpty ? "security unknown" : ap.security)
                        .font(.system(size: 9))
                        .foregroundStyle(.secondary)
                    Text("•")
                        .font(.system(size: 9))
                        .foregroundStyle(.tertiary)
                    Text("\(ap.rssi) dBm")
                        .font(.system(size: 9).monospaced())
                        .foregroundStyle(.secondary)
                }
            }

            Spacer()

            if ap.isSelected {
                Label("Connected", systemImage: "checkmark.circle.fill")
                    .font(.system(size: 10, weight: .semibold))
                    .foregroundStyle(.green)
            } else {
                Button("Connect") {
                    pendingSSID = ap.ssid
                    passphrase = ""
                    isShowingPassphrasePrompt = true
                }
                .buttonStyle(.bordered)
                .controlSize(.mini)
                .font(.caption2)
            }
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 5)
        .background(
            RoundedRectangle(cornerRadius: 6)
                .fill(ap.isSelected ? Color.green.opacity(0.08) : Color.secondary.opacity(0.04))
        )
    }

    private var footer: some View {
        HStack {
            Button(action: {
                openWindow(id: "dashboard")
                NSApp.activate(ignoringOtherApps: true)
            }) {
                HStack(spacing: 5) {
                    Image(systemName: "macwindow")
                    Text("Open Event Horizon")
                }
                .font(.caption.weight(.semibold))
            }
            .buttonStyle(.borderedProminent)

            Spacer()

            Button("Quit") {
                NSApplication.shared.terminate(nil)
            }
            .buttonStyle(.bordered)
            .controlSize(.small)
            .foregroundStyle(.secondary)
        }
    }

    private func stepLabel(_ text: String, systemImage: String) -> some View {
        Label(text, systemImage: systemImage)
            .font(.system(size: 9, weight: .bold))
            .foregroundStyle(.secondary)
    }

    // MARK: - Derived state

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

    private var selectedDeviceSubtitle: String {
        guard let node = selectedNode else { return "Restart Event Horizon" }
        if !node.networkTarget.isEmpty { return node.networkTarget }
        if !node.ipAddress.isEmpty { return node.ipAddress }
        return node.status
    }

    private var selectedDeviceIcon: String {
        guard let node = selectedNode else { return "exclamationmark.triangle" }
        return interfaceIcon(for: node)
    }

    private var connectableHotspots: [AccessPoint] {
        store.hotspots.filter {
            !$0.ssid.isEmpty
                && $0.ssid != "<hidden>"
                && $0.rssi > -85
        }
    }

    private func isWiredNode(_ node: HardwareTopologyNode?) -> Bool {
        guard let node else { return false }
        return node.usbDriver.localizedCaseInsensitiveContains("ethernet")
            || node.usbDriver.localizedCaseInsensitiveContains("lan")
            || node.usbDriver.localizedCaseInsensitiveContains("rtl8156")
    }

    private func interfaceIcon(for node: HardwareTopologyNode) -> String {
        if isWiredNode(node) { return "cable.connector" }
        if node.driverType.localizedCaseInsensitiveContains("wifi") { return "antenna.radiowaves.left.and.right" }
        return "laptopcomputer"
    }

    private func signalIcon(for rssi: Int8) -> String {
        if rssi > -70 { return "wifi" }
        return "wifi.exclamationmark"
    }
}
