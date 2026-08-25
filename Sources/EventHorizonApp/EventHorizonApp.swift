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
            if !store.isDaemonConnected {
                Image(systemName: "wifi.slash")
                Text("Offline")
            } else if store.isConnecting {
                Image(systemName: "wifi")
                    .symbolEffect(.pulse)
                Text("Connecting...")
            } else if !store.activeConnectedNodes.isEmpty {
                Image(systemName: "wifi")
                if store.activeConnectedNodes.count > 1 {
                    Text("\(store.activeConnectedNodes.first!.networkTarget) (+\(store.activeConnectedNodes.count - 1))")
                } else {
                    Text(store.activeConnectedNodes.first!.networkTarget)
                }
            } else if let ssid = store.primaryConnectedSSID {
                Image(systemName: "wifi")
                Text(ssid)
            } else {
                Image(systemName: "wifi")
                Text("Event Horizon")
            }
        }
    }
}
