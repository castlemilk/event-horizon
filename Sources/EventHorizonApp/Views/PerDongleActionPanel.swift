import SwiftUI
import EventHorizonCore

public struct PerDongleActionPanel: View {
    let node: HardwareTopologyNode
    let stat: InterfaceStat?

    public init(node: HardwareTopologyNode, stat: InterfaceStat?, pings: [PingResult], onSwitchWiFi: @escaping () -> Void) {
        self.node = node
        self.stat = stat
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Label(node.usbDriver, systemImage: node.category.systemIconName).font(.headline)
            Text(node.status).foregroundStyle(.secondary)
            DeviceDiagnosticsView(node: node, stat: stat)
        }
        .padding(14)
        .background(Color(nsColor: .windowBackgroundColor), in: RoundedRectangle(cornerRadius: 10))
    }
}
