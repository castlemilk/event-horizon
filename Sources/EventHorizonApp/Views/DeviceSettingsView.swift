import SwiftUI
import EventHorizonCore

public struct DeviceSettingsView: View {
    let node: HardwareTopologyNode?
    let onBack: () -> Void

    @State private var deviceName = "USB Wi-Fi 6E Adapter"
    @State private var autoConnect = true
    @State private var preferredBand = "5 GHz (Wi-Fi 6)"
    @State private var privateAddress = true

    public init(node: HardwareTopologyNode?, onBack: @escaping () -> Void) {
        self.node = node
        self.onBack = onBack
        if let n = node {
            self._deviceName = State(initialValue: n.usbDriver)
        }
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            // Header Back Link & Title
            VStack(alignment: .leading, spacing: 8) {
                Button(action: onBack) {
                    HStack(spacing: 4) {
                        Image(systemName: "chevron.left")
                        Text("Back")
                    }
                    .font(.body)
                    .foregroundStyle(.blue)
                }
                .buttonStyle(.plain)

                HStack(spacing: 12) {
                    Image(systemName: "wifi.circle.fill")
                        .font(.largeTitle)
                        .foregroundStyle(.blue)

                    VStack(alignment: .leading, spacing: 2) {
                        Text("Edit Device")
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.secondary)
                        Text(deviceName)
                            .font(.title2.weight(.bold))
                    }
                }
            }

            Divider()

            // Settings Form Card (Matching Frame 3 of mockup)
            VStack(alignment: .leading, spacing: 18) {
                // Device Name Field
                HStack {
                    Text("Device name")
                        .font(.body.weight(.medium))
                    Spacer()
                    TextField("Device Name", text: $deviceName)
                        .textFieldStyle(.roundedBorder)
                        .frame(width: 280)
                }

                Divider()

                // Auto-Connect Toggle
                Toggle(isOn: $autoConnect) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Auto-connect")
                            .font(.body.weight(.medium))
                        Text("Automatically connect to known networks.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }

                Divider()

                // Preferred Band Picker
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Preferred band")
                            .font(.body.weight(.medium))
                        Text("Choose the preferred Wi-Fi band.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }

                    Spacer()

                    Picker("", selection: $preferredBand) {
                        Text("5 GHz (Wi-Fi 6)").tag("5 GHz (Wi-Fi 6)")
                        Text("2.4 GHz").tag("2.4 GHz")
                        Text("Auto").tag("Auto")
                    }
                    .pickerStyle(.menu)
                    .frame(width: 180)
                }

                Divider()

                // Private Wi-Fi Address Toggle
                Toggle(isOn: $privateAddress) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Private Wi-Fi address")
                            .font(.body.weight(.medium))
                        Text("Use a randomized MAC address.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }

                Divider()

                // Action Buttons
                HStack {
                    Spacer()
                    Button("Cancel", action: onBack)
                        .buttonStyle(.bordered)

                    Button("Save") {
                        onBack()
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(.blue)
                }
            }
            .padding(20)
            .background(Color(nsColor: .windowBackgroundColor))
            .clipShape(RoundedRectangle(cornerRadius: 10))
            .overlay(
                RoundedRectangle(cornerRadius: 10)
                    .stroke(Color.secondary.opacity(0.12), lineWidth: 1)
            )

            Spacer()
        }
    }
}
