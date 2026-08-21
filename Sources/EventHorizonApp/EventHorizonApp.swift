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
            Image(systemName: store.isDaemonConnected ? "wifi" : "wifi.slash")
                .symbolRenderingMode(.palette)
                .foregroundStyle(store.isDaemonConnected ? .green : .primary)
        }
        .menuBarExtraStyle(.window)
    }
}
