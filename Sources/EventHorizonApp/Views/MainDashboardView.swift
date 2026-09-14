import SwiftUI
import EventHorizonCore

public struct MainDashboardView: View {
    @Bindable var store: WiFiManagerStore
    @State private var activeSection: NavigationSection = .overview
    @State private var showConnectSheet = false
    @State private var targetSSID = ""
    @State private var isEditingDevice = false
    @State private var selectedDeviceForDetail: HardwareTopologyNode?

    public init(store: WiFiManagerStore) {
        self.store = store
    }

    private var primaryNode: HardwareTopologyNode? {
        if let dongleID = store.selectedDongleId {
            return store.topologyNodes.first { HardwareTopologyNode.dongleId($0) == dongleID }
        }
        return store.topologyNodes.first { $0.interfaceName == store.selectedInterface }
    }

    private var secondaryNodes: [HardwareTopologyNode] {
        store.topologyNodes.filter { $0.id != primaryNode?.id }
    }

    public var body: some View {
        HStack(spacing: 0) {
            // Sidebar Navigation
            LinkPortSidebarView(selection: $activeSection)

            Divider()

            // Main Content Area
            VStack(spacing: 0) {
                // Unified Content Header
                HStack(alignment: .center) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(activeSection.rawValue)
                            .font(.title2.weight(.bold))
                        Text(sectionSubtitle(for: activeSection))
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }

                    Spacer()

                    // Quick Refresh Button
                    Button(action: {
                        Task { await store.refreshData() }
                    }) {
                        HStack(spacing: 5) {
                            Image(systemName: "arrow.clockwise")
                                .font(.system(size: 11, weight: .medium))
                            Text("Refresh")
                                .font(.caption.weight(.medium))
                        }
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }
                .padding(.horizontal, 24)
                .padding(.top, 18)
                .padding(.bottom, 12)
                .background(Color(nsColor: .windowBackgroundColor))

                Divider()

                HStack(spacing: 8) {
                    if store.isStartingDaemon {
                        ProgressView().controlSize(.small)
                    } else {
                        Image(systemName: store.isDaemonConnected ? "checkmark.circle" : "exclamationmark.triangle")
                            .foregroundStyle(store.isDaemonConnected ? .green : .orange)
                    }
                    Text(store.statusMessage).font(.callout).textSelection(.enabled)
                    Spacer()
                    if !store.isDaemonConnected && !store.isStartingDaemon {
                        Button("Retry Connection") { Task { await store.retryDaemonConnection() } }
                    }
                }
                .padding(.horizontal, 24)
                .padding(.vertical, 10)

                ScrollView {
                    VStack(alignment: .leading, spacing: 16) {
                        switch activeSection {
                        case .overview:
                            OverviewDashboardView(
                                node: primaryNode,
                                otherNodes: secondaryNodes,
                                stat: store.selectedTelemetry,
                                activeHotspot: store.activeHotspotForSelectedInterface,
                                rxHistory: store.rxHistory,
                                txHistory: store.txHistory,
                                onSelectDevice: { device in
                                    store.selectDeviceInterface(device.interfaceName)
                                    selectedDeviceForDetail = device
                                    activeSection = .devices
                                },
                                onDisconnect: {
                                    Task { await store.disconnect() }
                                }
                            )

                        case .devices:
                            if isEditingDevice {
                                DeviceSettingsView(node: primaryNode, onBack: { isEditingDevice = false })
                            } else {
                                HardwareMappingView(
                                    nodes: store.topologyNodes,
                                    hotspots: store.hotspots,
                                    interfaceStats: store.interfaceStats,
                                    pings: store.pingResults,
                                    selectedDevice: $selectedDeviceForDetail,
                                    onSelectHotspot: { ssid in
                                        self.targetSSID = ssid
                                        self.showConnectSheet = true
                                    }
                                )
                            }

                        case .wifi:
                            EndpointListView(
                                hotspots: store.hotspots,
                                onSelect: { ap in
                                    self.targetSSID = ap.ssid
                                    self.showConnectSheet = true
                                }
                            )

                        case .terminal:
                            TerminalView(store: store)

                        case .spectrum:
                            RFSpectrumAnalyzerView(store: store)

                        case .routing:
                            MultiInterfaceRoutingView(store: store)

                        case .metrics:
                            LiveMetricsAnalyticsView(
                                stat: store.selectedTelemetry,
                                pings: store.pingResults,
                                hotspot: store.activeHotspotForSelectedInterface,
                                interfaces: store.topologyNodes.map(\.bsdInterface).filter { !$0.isEmpty },
                                selectedInterface: store.selectedInterface,
                                signalHistory: store.signalHistory,
                                latencyHistory: store.latencyHistory,
                                rxHistory: store.rxHistory,
                                txHistory: store.txHistory,
                                onSelectInterface: { iface in
                                    store.selectDeviceInterface(iface)
                                }
                            )

                        case .diagnostics:
                            VStack(spacing: 16) {
                                SpeedtestView(store: store)
                                WiFiDiagnosticView(store: store)
                            }

                        case .updates:
                            FirmwareUpdatesView(
                                store: store,
                                node: primaryNode,
                                otherNodes: secondaryNodes
                            )

                        case .settings:
                            DeviceSettingsView(node: primaryNode, onBack: { activeSection = .overview })
                        }
                    }
                    .padding(24)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color(nsColor: .underPageBackgroundColor))
        }
        .frame(minWidth: 920, minHeight: 640)
        .sheet(isPresented: $showConnectSheet) {
            ConnectHotspotSheet(
                ssid: targetSSID,
                isConnecting: store.isConnecting,
                onConnect: { passphrase in
                    Task {
                        if await store.connect(to: targetSSID, passphrase: passphrase) {
                            showConnectSheet = false
                        }
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

    private func sectionSubtitle(for section: NavigationSection) -> String {
        switch section {
        case .overview:
            return "Active network adapter, live throughput & attached hardware"
        case .devices:
            return "Multi-dongle topology, USB bus controllers & per-device controls"
        case .wifi:
            return "In-range 802.11 Wi-Fi access points & network connections"
        case .terminal:
            return "Real-time celestial obstruction radar (101×101 SNR grid), satellite tracking & terminal controls"
        case .spectrum:
            return "2.4 GHz & 5 GHz RF channel occupancy, interference & congestion heatmaps"
        case .routing:
            return "Multi-WAN interface prioritization & automated zero-stall link failover"
        case .metrics:
            return "Live signal strength, gateway latency RTT & bandwidth analytics"
        case .diagnostics:
            return "Per-adapter speed tests, connectivity, DNS latency & link scoring"
        case .updates:
            return "DriverKit dext extensions, BootROM & firmware lifecycle"
        case .settings:
            return "Device configuration, ZeroCD modeswitch, and interface rules"
        }
    }
}
