import SwiftUI
import StarlinkWiFiCore

public struct MenuBarPopoverView: View {
    @Bindable var store: WiFiManagerStore
    @Environment(\.openWindow) private var openWindow

    public init(store: WiFiManagerStore) {
        self.store = store
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // Header
            HStack {
                Image(systemName: "wifi")
                    .foregroundStyle(store.isDaemonConnected ? .green : .orange)
                Text("USB WiFi Manager")
                    .font(.headline)
                Spacer()
                Text(store.isDaemonConnected ? "ONLINE" : "OFFLINE")
                    .font(.system(size: 9, weight: .bold))
                    .foregroundStyle(store.isDaemonConnected ? .green : .red)
            }

            Divider()

            // Active Connection
            if let active = store.selectedHotspot {
                VStack(alignment: .leading, spacing: 4) {
                    Text("ACTIVE ENDPOINT")
                        .font(.caption2.weight(.bold))
                        .foregroundStyle(.secondary)

                    HStack {
                        VStack(alignment: .leading) {
                            Text(active.ssid)
                                .font(.body.weight(.semibold))
                            Text(active.bssid)
                                .font(.caption2.monospaced())
                                .foregroundStyle(.secondary)
                        }
                        Spacer()
                        Image(systemName: "checkmark.circle.fill")
                            .foregroundStyle(.green)
                    }
                }
                .padding(8)
                .background(Color.green.opacity(0.1))
                .clipShape(RoundedRectangle(cornerRadius: 6))
            } else {
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(store.isDaemonConnected ? "USB Wi-Fi Ready" : "Daemon Connecting...")
                            .font(.caption.weight(.semibold))
                        Text(store.statusMessage)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    if !store.isDaemonConnected {
                        ProgressView()
                            .controlSize(.small)
                    }
                }
                .padding(8)
                .background(Color.secondary.opacity(0.1))
                .clipShape(RoundedRectangle(cornerRadius: 6))
            }

            // Quick Hotspots List
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    Text("DISCOVERED ENDPOINTS")
                        .font(.caption2.weight(.bold))
                        .foregroundStyle(.secondary)
                    Spacer()
                    Button(action: {
                        Task { await store.refreshData() }
                    }) {
                        Image(systemName: "arrow.clockwise")
                            .font(.caption2)
                    }
                    .buttonStyle(.plain)
                }

                ForEach(store.hotspots.prefix(4)) { ap in
                    HStack {
                        VStack(alignment: .leading, spacing: 1) {
                            Text(ap.ssid)
                                .font(.caption.weight(.medium))
                            Text("\(ap.security) • Ch \(ap.channel)")
                                .font(.system(size: 8))
                                .foregroundStyle(.secondary)
                        }
                        Spacer()
                        if ap.isSelected {
                            Image(systemName: "checkmark")
                                .font(.caption)
                                .foregroundStyle(.green)
                        } else {
                            Button("Connect") {
                                Task { await store.connect(to: ap.ssid) }
                            }
                            .buttonStyle(.borderless)
                            .font(.caption)
                        }
                    }
                    .padding(.vertical, 2)
                }

                if store.hotspots.isEmpty {
                    Text("Scanning for networks...")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }

            Divider()

            // Footer Actions
            HStack {
                Button("Open Dashboard") {
                    openWindow(id: "dashboard")
                    NSApp.activate(ignoringOtherApps: true)
                }
                .buttonStyle(.borderedProminent)

                Spacer()

                Button("Quit") {
                    NSApplication.shared.terminate(nil)
                }
                .buttonStyle(.bordered)
            }
        }
        .padding(14)
        .frame(width: 320)
    }
}
