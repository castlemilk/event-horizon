import SwiftUI
import StarlinkWiFiCore

public struct MainDashboardView: View {
    @Bindable var store: WiFiManagerStore
    @State private var activeSection: NavigationSection = .overview
    @State private var showConnectSheet = false
    @State private var targetSSID = ""
    @State private var isEditingDevice = false

    public init(store: WiFiManagerStore) {
        self.store = store
    }

    private var primaryNode: HardwareTopologyNode? {
        store.topologyNodes.first(where: { $0.usbDriver.contains("Wi-Fi") || $0.usbDriver.contains("WLAN") }) ?? store.topologyNodes.first
    }

    private var secondaryNodes: [HardwareTopologyNode] {
        store.topologyNodes.filter { $0.id != primaryNode?.id }
    }

    public var body: some View {
        HStack(spacing: 0) {
            // Sidebar Navigation (Matching exact LinkPort sidebar in reference mockup)
            LinkPortSidebarView(selection: $activeSection)

            Divider()

            // Main Content Area
            VStack(spacing: 0) {
                ScrollView {
                    VStack(alignment: .leading, spacing: 16) {
                        switch activeSection {
                        case .overview:
                            // 1. Overview Screen (Matching Frame 1 of mockup)
                            OverviewDashboardView(
                                node: primaryNode,
                                otherNodes: secondaryNodes,
                                stat: store.interfaceStats.first
                            )

                        case .devices:
                            if isEditingDevice {
                                // 3. Device Settings Screen (Matching Frame 3 of mockup)
                                DeviceSettingsView(node: primaryNode, onBack: { isEditingDevice = false })
                            } else {
                                // 2. Per-Device Management Hierarchy Cards View
                                HardwareMappingView(
                                    nodes: store.topologyNodes,
                                    hotspots: store.hotspots,
                                    interfaceStats: store.interfaceStats,
                                    pings: store.pingResults,
                                    onSelectHotspot: { ssid in
                                        self.targetSSID = ssid
                                        self.showConnectSheet = true
                                    }
                                )
                            }

                        case .wifi:
                            // 2. Wi-Fi Networks & Connect Screen (Matching Frame 2 of mockup)
                            EndpointListView(
                                hotspots: store.hotspots,
                                onSelect: { ap in
                                    self.targetSSID = ap.ssid
                                    self.showConnectSheet = true
                                }
                            )

                        case .metrics:
                            // 5. Live Connection Metrics Screen (Matching Frame 5 of mockup)
                            LiveMetricsAnalyticsView(
                                stat: store.interfaceStats.first,
                                pings: store.pingResults
                            )

                        case .updates:
                            // 4. Firmware / Driver Updates Screen (Matching Frame 4 of mockup)
                            FirmwareUpdatesView(
                                node: primaryNode,
                                otherNodes: secondaryNodes
                            )

                        case .settings:
                            // 3. Settings Screen (Matching Frame 3 of mockup)
                            DeviceSettingsView(node: primaryNode, onBack: { activeSection = .overview })
                        }
                    }
                    .padding(20)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color(nsColor: .underPageBackgroundColor))
        }
        .frame(minWidth: 920, minHeight: 620)
        .sheet(isPresented: $showConnectSheet) {
            ConnectHotspotSheet(
                ssid: targetSSID,
                isConnecting: store.isConnecting,
                onConnect: { passphrase in
                    Task {
                        await store.connect(to: targetSSID, passphrase: passphrase)
                        showConnectSheet = false
                    }
                },
                onDismiss: {
                    showConnectSheet = false
                }
            )
        }
        .task {
            await store.bootstrap()
        }
    }
}
