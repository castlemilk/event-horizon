import SwiftUI
import StarlinkWiFiCore

public struct ConnectHotspotSheet: View {
    let ssid: String
    let isConnecting: Bool
    let onConnect: (String) -> Void
    let onDismiss: () -> Void

    @State private var passphrase = ""

    public init(ssid: String, isConnecting: Bool, onConnect: @escaping (String) -> Void, onDismiss: @escaping () -> Void) {
        self.ssid = ssid
        self.isConnecting = isConnecting
        self.onConnect = onConnect
        self.onDismiss = onDismiss
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Image(systemName: "wifi.lock")
                    .font(.title)
                    .foregroundStyle(Color.accentColor)

                VStack(alignment: .leading, spacing: 2) {
                    Text("Connect USB Dongle to Network")
                        .font(.headline)
                    Text("SSID: \(ssid)")
                        .font(.subheadline.monospaced())
                        .foregroundStyle(.secondary)
                }

                Spacer()

                Button(action: onDismiss) {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)
            }

            Divider()

            VStack(alignment: .leading, spacing: 6) {
                Text("Wi-Fi Passphrase (WPA2/WPA3)")
                    .font(.caption.weight(.semibold))

                SecureField("Enter WPA2 passphrase...", text: $passphrase)
                    .textFieldStyle(.roundedBorder)
                    .disabled(isConnecting)
            }

            HStack {
                Spacer()

                Button("Cancel", action: onDismiss)
                    .buttonStyle(.bordered)
                    .disabled(isConnecting)

                Button(action: {
                    onConnect(passphrase)
                }) {
                    HStack {
                        if isConnecting {
                            ProgressView()
                                .controlSize(.small)
                        }
                        Text(isConnecting ? "Authenticating..." : "Connect Dongle")
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(isConnecting)
            }
        }
        .padding(20)
        .frame(width: 420)
    }
}
