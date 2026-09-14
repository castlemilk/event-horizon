import SwiftUI
import EventHorizonCore

/// Keeps the app alive when its window closes.
///
/// This app is a menu-bar app that also has a dashboard window. SwiftUI
/// terminates an app whose last WindowGroup window closes, which took the
/// MenuBarExtra down with it: closing the dashboard quit the whole app, the
/// icon vanished from the menu bar, and because it is an orderly quit rather
/// than a crash there was no crash report and nothing in the log to explain it.
/// It simply was not there any more.
///
/// The daemon is unaffected — it is a separate root process — so the link keeps
/// working while the app that is supposed to be supervising it is gone, which
/// is the worst version of this: everything looks fine except the thing you
/// look at.
final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false
    }
}

@main
struct EventHorizonApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @State private var store = WiFiManagerStore(
        manageDaemon: ProcessInfo.processInfo.environment["EVENT_HORIZON_OBSERVE_ONLY"] != "1"
    )

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
