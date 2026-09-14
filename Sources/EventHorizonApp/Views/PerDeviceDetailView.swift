import SwiftUI
import EventHorizonCore

public struct PerDeviceDetailView: View {
    let node: HardwareTopologyNode
    let hotspots: [AccessPoint]
    let stat: InterfaceStat?
    let pings: [PingResult]
    let onBack: () -> Void
    let onSelectHotspot: (String) -> Void
    @State private var selectedTab = 0

    public init(node: HardwareTopologyNode, hotspots: [AccessPoint], stat: InterfaceStat?, pings: [PingResult], onBack: @escaping () -> Void, onSelectHotspot: @escaping (String) -> Void) {
        self.node = node
        self.hotspots = hotspots
        self.stat = stat
        self.pings = pings
        self.onBack = onBack
        self.onSelectHotspot = onSelectHotspot
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack {
                Button("All Devices", systemImage: "chevron.left", action: onBack)
                Spacer()
                Label(node.routeBadge, systemImage: node.isConnected ? "checkmark.circle" : "info.circle")
                    .foregroundStyle(node.isConnected ? .green : .secondary)
            }
            HStack(spacing: 16) {
                Image(systemName: node.category.systemIconName).font(.largeTitle).foregroundStyle(.tint)
                VStack(alignment: .leading, spacing: 4) {
                    Text(node.usbDriver).font(.title2.bold())
                    Text(node.status).foregroundStyle(.secondary)
                    Text("Interface: \(node.interfaceName.isEmpty ? "Unavailable" : node.interfaceName) · IP: \(node.ipAddress.isEmpty ? "Unassigned" : node.ipAddress)")
                        .font(.caption.monospaced())
                }
            }
            Picker("Device details", selection: $selectedTab) {
                Text("Telemetry").tag(0)
                Text("Diagnostics").tag(1)
                Text("Hardware").tag(2)
            }.pickerStyle(.segmented)
            switch selectedTab {
            case 0: DeviceTelemetryView(node: node, stat: stat)
            case 1: DeviceDiagnosticsView(node: node, stat: stat)
            default: DeviceHardwareDetailsView(node: node)
            }
        }
        .padding(20)
        .background(Color(nsColor: .windowBackgroundColor), in: RoundedRectangle(cornerRadius: 12))
    }
}

struct DeviceHardwareDetailsView: View {
    let node: HardwareTopologyNode
    var body: some View {
        VStack(spacing: 8) {
            DetailGridRow(label: "Network", value: node.isConnected && !node.networkTarget.isEmpty ? node.networkTarget : "Not connected / unavailable")
            DetailGridRow(label: "Vendor / Product", value: "\(node.vendorId) / \(node.productId)")
            DetailGridRow(label: "Serial number", value: node.serialNumber.isEmpty ? "Not reported" : node.serialNumber)
            DetailGridRow(label: "USB bus path", value: node.busPath ?? "Not reported")
            DetailGridRow(label: "Bus speed", value: node.speed.isEmpty ? "Not reported" : node.speed)
            DetailGridRow(label: "Interface", value: node.interfaceName.isEmpty ? "Unavailable" : node.interfaceName)
            DetailGridRow(label: "IP address", value: node.ipAddress.isEmpty ? "Unassigned" : node.ipAddress)
            DetailGridRow(label: "Subnet", value: node.subnetMask.isEmpty ? "Not reported" : node.subnetMask)
            DetailGridRow(label: "Gateway", value: node.gateway.isEmpty ? "Not reported" : node.gateway)
            DetailGridRow(label: "MAC address", value: node.macAddress.isEmpty ? "Not reported" : node.macAddress)
            DetailGridRow(label: "Driver", value: node.driverType.isEmpty ? "Not reported" : node.driverType)
        }
        .textSelection(.enabled)
    }
}

// DetailGridRow is the label/value row the hardware-details grid is built
// from. Was removed mid-edit while its usages stayed; restoring it.
public struct DetailGridRow: View {
    let label: String
    let value: String

    public init(label: String, value: String) {
        self.label = label
        self.value = value
    }

    public var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
            Spacer()
            Text(value)
                .font(.caption.monospaced())
                .multilineTextAlignment(.trailing)
        }
    }
}

public struct DeviceGraphicView: View {
    let deviceDriver: String

    public init(deviceDriver: String) {
        self.deviceDriver = deviceDriver
    }

    public var body: some View {
        if deviceDriver.contains("Apple Silicon") || deviceDriver.contains("Broadcom") || deviceDriver.contains("Built-in") {
            AppleSiliconChipGraphicView()
        } else if deviceDriver.contains("Ethernet") || deviceDriver.contains("RTL8156") {
            ZStack {
                RoundedRectangle(cornerRadius: 10)
                    .fill(LinearGradient(colors: [Color.blue.opacity(0.8), Color.blue], startPoint: .topLeading, endPoint: .bottomTrailing))
                Image(systemName: "cable.connector.horizontal")
                    .font(.system(size: 32))
                    .foregroundStyle(.white)
            }
        } else {
            USBDongleVectorView()
                .scaleEffect(0.5)
        }
    }
}
