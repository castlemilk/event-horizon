import SwiftUI
import StarlinkWiFiCore

@main
struct StarlinkWiFiApp: App {
    @State private var store = WiFiManagerStore()

    var body: some Scene {
        WindowGroup("USB WiFi Dashboard", id: "dashboard") {
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
