import SwiftUI
import StarlinkWiFiCore

public struct MenuBarPopoverView: View {
    @Bindable var store: WiFiManagerStore
    @Environment(\.openWindow) private var openWindow
    @State private var isPerformingPing = false
    @State private var isPerformingSpeedtest = false

    public init(store: WiFiManagerStore) {
        self.store = store
    }

    private func safeLoadBlackholeLogo() -> NSImage? {
        let fileManager = FileManager.default
        if let mainResPath = Bundle.main.path(forResource: "blackhole_logo", ofType: "jpg") {
            return NSImage(contentsOfFile: mainResPath)
        }
        let bundlePath = Bundle.main.bundlePath
        let resPath = (bundlePath as NSString).appendingPathComponent("Contents/Resources/blackhole_logo.jpg")
        if fileManager.fileExists(atPath: resPath) {
            return NSImage(contentsOfFile: resPath)
        }
        if fileManager.fileExists(atPath: "Sources/StarlinkWiFiApp/Resources/blackhole_logo.jpg") {
            return NSImage(contentsOfFile: "Sources/StarlinkWiFiApp/Resources/blackhole_logo.jpg")
        }
        return nil
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            // Header: Event Horizon Branding & Status
            HStack(spacing: 10) {
                ZStack {
                    RoundedRectangle(cornerRadius: 6)
                        .fill(Color.black)
                        .frame(width: 28, height: 28)
                    if let logo = safeLoadBlackholeLogo() {
                        Image(nsImage: logo)
                            .resizable()
                            .scaledToFill()
                            .clipShape(RoundedRectangle(cornerRadius: 6))
                            .frame(width: 28, height: 28)
                    } else {
                        Image(systemName: "circle.circle.fill")
                            .font(.system(size: 14, weight: .bold))
                            .foregroundStyle(.cyan)
                    }
                }

                VStack(alignment: .leading, spacing: 1) {
                    Text("Event Horizon")
                        .font(.headline.weight(.bold))
                    Text("Quick-Action Manager")
                        .font(.system(size: 9, weight: .medium))
                        .foregroundStyle(.secondary)
                }

                Spacer()

                HStack(spacing: 6) {
                    Circle()
                        .fill(store.isDaemonConnected ? Color.green : Color.red)
                        .frame(width: 6, height: 6)
                        .shadow(color: store.isDaemonConnected ? Color.green.opacity(0.6) : Color.red.opacity(0.6), radius: 2)

                    Text(store.isDaemonConnected ? "ONLINE" : "OFFLINE")
                        .font(.system(size: 9, weight: .bold))
                        .foregroundStyle(store.isDaemonConnected ? .green : .red)

                    Button(action: {
                        Task { await store.refreshData() }
                    }) {
                        Image(systemName: "arrow.clockwise")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.plain)
                    .help("Refresh Devices & Wi-Fi Scan")
                }
            }

            // Active Connection Card for Currently Selected Interface
            let activeAP = store.activeHotspotForSelectedInterface
            let selectedNode = store.topologyNodes.first(where: { $0.bsdInterface == store.selectedInterface })
            let isWired = selectedNode?.usbDriver.contains("Ethernet") == true || selectedNode?.usbDriver.contains("RTL8156") == true

            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 4) {
                    Text(isWired ? "ACTIVE WIRED LINK (\(store.selectedInterface))" : "ACTIVE NETWORK (\(store.selectedInterface))")
                        .font(.system(size: 8, weight: .bold))
                        .foregroundStyle(.secondary)
                    Spacer()
                    Text(isWired ? "2.5G WIRED LINK" : activeAP.security)
                        .font(.system(size: 8, weight: .semibold))
                        .foregroundStyle(.green)
                }

                HStack(spacing: 8) {
                    Image(systemName: isWired ? "cable.connector" : "wifi")
                        .font(.title3)
                        .foregroundStyle(.green)

                    VStack(alignment: .leading, spacing: 1) {
                        Text(activeAP.ssid)
                            .font(.body.weight(.bold))
                        if isWired {
                            Text("\(activeAP.bssid) • 2.5 Gbps Full-Duplex")
                                .font(.system(size: 9).monospaced())
                                .foregroundStyle(.secondary)
                        } else {
                            Text("\(activeAP.bssid) • \(activeAP.rssi) dBm • Ch \(activeAP.channel)")
                                .font(.system(size: 9).monospaced())
                                .foregroundStyle(.secondary)
                        }
                    }

                    Spacer()

                    Image(systemName: "checkmark.seal.fill")
                        .font(.title3)
                        .foregroundStyle(.green)
                }
            }
            .padding(10)
            .background(Color.green.opacity(0.12))
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .overlay(RoundedRectangle(cornerRadius: 8).stroke(Color.green.opacity(0.3), lineWidth: 1))

            Divider()

            // Quick-Action Section 1: Select Active Hardware Device
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    Label("1. SELECT DEVICE INTERFACE", systemImage: "cpu.fill")
                        .font(.system(size: 9, weight: .bold))
                        .foregroundStyle(.secondary)
                    Spacer()
                    Text("\(store.topologyNodes.count) Available")
                        .font(.system(size: 9, weight: .medium))
                        .foregroundStyle(.tertiary)
                }

                VStack(spacing: 4) {
                    ForEach(store.topologyNodes) { node in
                        let isSelected = store.selectedInterface == node.bsdInterface
                        Button(action: {
                            store.selectDeviceInterface(node.bsdInterface)
                        }) {
                            HStack(spacing: 8) {
                                Image(systemName: node.bsdInterface.contains("en0") ? "laptopcomputer" : ((node.usbDriver.contains("Ethernet") || node.usbDriver.contains("RTL8156")) ? "cable.connector" : "antenna.radiowaves.left.and.right"))
                                    .font(.caption)
                                    .foregroundStyle(isSelected ? Color.blue : Color.secondary)

                                VStack(alignment: .leading, spacing: 1) {
                                    HStack(spacing: 4) {
                                        Text(node.usbDriver)
                                            .font(.caption.weight(isSelected ? .semibold : .regular))
                                            .lineLimit(1)
                                        Text("(\(node.bsdInterface))")
                                            .font(.system(size: 9, weight: .bold).monospaced())
                                            .foregroundStyle(isSelected ? Color.blue : Color.secondary)
                                    }
                                    Text("\(node.ipAddress) • \(node.speed)")
                                        .font(.system(size: 8))
                                        .foregroundStyle(.secondary)
                                }

                                Spacer()

                                if isSelected {
                                    Image(systemName: "checkmark.circle.fill")
                                        .font(.caption)
                                        .foregroundStyle(.blue)
                                }
                            }
                            .padding(.horizontal, 8)
                            .padding(.vertical, 6)
                            .background(
                                RoundedRectangle(cornerRadius: 6)
                                    .fill(isSelected ? Color.blue.opacity(0.12) : Color.secondary.opacity(0.06))
                            )
                        }
                        .buttonStyle(.plain)
                    }
                }
            }

            Divider()

            // Quick-Action Section 2: Select & Connect to Wi-Fi Network
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    Label("2. SELECT WI-FI NETWORK", systemImage: "wifi")
                        .font(.system(size: 9, weight: .bold))
                        .foregroundStyle(.secondary)
                    Spacer()
                    if store.isConnecting {
                        ProgressView()
                            .controlSize(.small)
                    }
                }

                VStack(spacing: 4) {
                    ForEach(store.hotspots.prefix(5)) { ap in
                        let isSelected = ap.ssid == activeAP.ssid
                        HStack(spacing: 8) {
                            Image(systemName: signalIcon(for: ap.rssi))
                                .font(.caption)
                                .foregroundStyle(isSelected ? Color.green : Color.primary.opacity(0.8))

                            VStack(alignment: .leading, spacing: 1) {
                                Text(ap.ssid)
                                    .font(.caption.weight(isSelected ? .semibold : .regular))
                                HStack(spacing: 4) {
                                    Text("\(ap.security)")
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

                            if isSelected {
                                HStack(spacing: 4) {
                                    Text("CONNECTED")
                                        .font(.system(size: 8, weight: .bold))
                                        .foregroundStyle(.green)
                                    Image(systemName: "checkmark")
                                        .font(.caption2.weight(.bold))
                                        .foregroundStyle(.green)
                                }
                                .padding(.horizontal, 6)
                                .padding(.vertical, 3)
                                .background(Color.green.opacity(0.12))
                                .clipShape(Capsule())
                            } else {
                                Button("Connect") {
                                    Task {
                                        await store.connect(to: ap.ssid)
                                    }
                                }
                                .buttonStyle(.borderedProminent)
                                .controlSize(.small)
                                .font(.caption2)
                            }
                        }
                        .padding(.horizontal, 8)
                        .padding(.vertical, 5)
                        .background(
                            RoundedRectangle(cornerRadius: 6)
                                .fill(isSelected ? Color.green.opacity(0.08) : Color.secondary.opacity(0.04))
                        )
                    }

                    if store.hotspots.isEmpty {
                        Text("Scanning 802.11 beacons...")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                            .padding(.vertical, 4)
                    }
                }
            }

            Divider()

            // Quick Diagnostic Bar
            HStack(spacing: 6) {
                // Configurable Target Menu
                Menu {
                    Button("1.1.1.1 (Cloudflare DNS)") { store.pingTargetHost = "1.1.1.1" }
                    Button("8.8.8.8 (Google DNS)") { store.pingTargetHost = "8.8.8.8" }
                    Button("9.9.9.9 (Quad9 DNS)") { store.pingTargetHost = "9.9.9.9" }
                    Button("192.168.1.1 (Local Gateway)") { store.pingTargetHost = "192.168.1.1" }
                } label: {
                    HStack(spacing: 2) {
                        Text(store.pingTargetHost)
                            .font(.system(size: 9, weight: .bold).monospaced())
                        Image(systemName: "chevron.down")
                            .font(.system(size: 8))
                    }
                    .padding(.horizontal, 4)
                    .padding(.vertical, 3)
                }
                .menuStyle(.borderlessButton)
                .fixedSize()

                Button(action: {
                    Task {
                        await store.runPingDiagnostic(target: store.pingTargetHost)
                    }
                }) {
                    HStack(spacing: 4) {
                        if store.isPinging {
                            ProgressView()
                                .controlSize(.small)
                            Text("Pinging...")
                        } else if store.lastPingSuccess == true {
                            Image(systemName: "checkmark.circle.fill")
                                .foregroundStyle(.green)
                            Text("Ping OK (\(store.lastPingRTTMs)ms)")
                        } else if store.lastPingSuccess == false {
                            Image(systemName: "xmark.circle.fill")
                                .foregroundStyle(.red)
                            Text("Ping Failed")
                        } else {
                            Image(systemName: "bolt.horizontal.fill")
                            Text("Ping Target")
                        }
                    }
                    .font(.caption2.weight(.semibold))
                    .frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
                .disabled(store.isPinging)

                Button(action: {
                    isPerformingSpeedtest = true
                    Task {
                        await store.refreshData()
                        isPerformingSpeedtest = false
                    }
                }) {
                    HStack(spacing: 4) {
                        Image(systemName: "speedometer")
                        Text(isPerformingSpeedtest ? "Testing..." : "Speed Test")
                    }
                    .font(.caption2.weight(.semibold))
                    .frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
                .disabled(isPerformingSpeedtest)
            }

            Divider()

            // Footer Actions
            HStack {
                Button(action: {
                    openWindow(id: "dashboard")
                    NSApp.activate(ignoringOtherApps: true)
                }) {
                    HStack(spacing: 6) {
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
        .padding(14)
        .frame(width: 330)
    }

    private func signalIcon(for rssi: Int8) -> String {
        if rssi > -55 {
            return "wifi"
        } else if rssi > -70 {
            return "wifi"
        } else {
            return "wifi.exclamationmark"
        }
    }
}
