import SwiftUI
import EventHorizonCore

public struct FirmwareUpdatesView: View {
    let node: HardwareTopologyNode?
    let otherNodes: [HardwareTopologyNode]

    public init(node: HardwareTopologyNode?, otherNodes: [HardwareTopologyNode]) {
        self.node = node
        self.otherNodes = otherNodes
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Text("Updates")
                .font(.title2.weight(.bold))

            // Active Device Card (Matching Frame 4 of mockup)
            if let active = node {
                HStack(spacing: 16) {
                    Image(systemName: "wifi.circle.fill")
                        .font(.system(size: 40))
                        .foregroundStyle(.blue)

                    VStack(alignment: .leading, spacing: 4) {
                        HStack(spacing: 8) {
                            Text(active.usbDriver)
                                .font(.headline)
                        }

                        Text("Firmware version reporting is not available for this device yet.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }

                    Spacer()
                }
                .padding(18)
                .background(Color(nsColor: .windowBackgroundColor))
                .clipShape(RoundedRectangle(cornerRadius: 10))
                .overlay(
                    RoundedRectangle(cornerRadius: 10)
                        .stroke(Color.secondary.opacity(0.12), lineWidth: 1)
                )
            }

            // Devices Section
            VStack(alignment: .leading, spacing: 10) {
                Text("Attached devices")
                    .font(.headline)

                VStack(spacing: 8) {
                    ForEach(otherNodes) { device in
                        HStack {
                            Image(systemName: deviceIcon(device.usbDriver))
                                .font(.title3)
                                .foregroundStyle(.secondary)
                                .frame(width: 28)

                            VStack(alignment: .leading, spacing: 2) {
                                Text(device.usbDriver)
                                    .font(.body.weight(.medium))
                                Text("Firmware version unknown")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }

                            Spacer()

                            HStack(spacing: 4) {
                                Text("No update data")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                                Image(systemName: "questionmark.circle")
                                    .foregroundStyle(.secondary)
                            }
                        }
                        .padding(12)
                        .background(Color(nsColor: .controlBackgroundColor))
                        .clipShape(RoundedRectangle(cornerRadius: 8))
                        .overlay(
                            RoundedRectangle(cornerRadius: 8)
                                .stroke(Color.secondary.opacity(0.12), lineWidth: 1)
                        )
                    }
                }
            }

            Spacer()
        }
    }

    private func deviceIcon(_ name: String) -> String {
        if name.contains("Apple Silicon") || name.contains("Built-in") || name.contains("Broadcom") { return "laptopcomputer" }
        if name.contains("Wi-Fi") { return "wifi" }
        if name.contains("Ethernet") { return "network" }
        return "point.3.connected.trianglepath.dotted"
    }
}
