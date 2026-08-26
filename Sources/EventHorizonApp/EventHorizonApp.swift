import SwiftUI
import EventHorizonCore

@main
struct EventHorizonApp: App {
    @State private var store = WiFiManagerStore()

    var body: some Scene {
        WindowGroup("Event Horizon", id: "dashboard") {
            MainDashboardView(store: store)
        }
        .windowStyle(.titleBar)
        .windowToolbarStyle(.unified)

        MenuBarExtra {
            MenuBarPopoverView(store: store)
        } label: {
            MenuBarLabelView(store: store)
        }
        .menuBarExtraStyle(.window)
    }
}

struct MenuBarLabelView: View {
    let store: WiFiManagerStore

    var body: some View {
        HStack(spacing: 4) {
            Image(systemName: iconName)
            if let ssid = store.primaryConnectedSSID, !ssid.isEmpty {
                Text(ssid)
                    .font(.caption2.weight(.medium))
            }
        }
    }

    private var iconName: String {
        if !store.isDaemonConnected {
            return "wifi.slash"
        } else if store.isConnecting {
            return "wifi.badge.plus"
        } else if !store.activeConnectedNodes.isEmpty || store.primaryConnectedSSID != nil {
            return "antenna.radiowaves.left.and.right"
        } else {
            return "wifi"
        }
    }
}
