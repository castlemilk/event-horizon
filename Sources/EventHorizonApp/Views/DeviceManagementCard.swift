import SwiftUI
import EventHorizonCore

public struct DeviceManagementCard: View {
    let node: HardwareTopologyNode
    let stat: InterfaceStat?

    public init(node: HardwareTopologyNode, hotspots: [AccessPoint], stat: InterfaceStat?, pings: [PingResult], onSelectHotspot: @escaping (String) -> Void) {
        self.node = node
        self.stat = stat
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            DeviceTelemetryView(node: node, stat: stat)
            Divider()
            DeviceDiagnosticsView(node: node, stat: stat)
            DisclosureGroup("Hardware details") {
                DeviceHardwareDetailsView(node: node).padding(.top, 12)
            }
        }
    }
}
