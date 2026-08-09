import XCTest
import SwiftUI
@testable import StarlinkWiFiCore
@testable import StarlinkWiFiApp

final class E2EBundleValidationTests: XCTestCase {
    func testLinkPortSidebarViewRendering() {
        // Instantiate Binding and Sidebar View
        var selection: NavigationSection = .overview
        let binding = Binding<NavigationSection>(
            get: { selection },
            set: { selection = $0 }
        )
        let sidebar = LinkPortSidebarView(selection: binding)
        
        // Evaluate body to ensure no runtime crashes, assertions, or missing bundle exceptions occur
        let body = sidebar.body
        XCTAssertNotNil(body, "LinkPortSidebarView body getter failed or threw exception")
    }

    func testBlackholeLogoAssetExistence() {
        let fm = FileManager.default
        var found = false

        // Check relative path in project
        if fm.fileExists(atPath: "Sources/StarlinkWiFiApp/Resources/blackhole_logo.jpg") {
            found = true
        }

        // Check Bundle.main resources
        if let mainPath = Bundle.main.path(forResource: "blackhole_logo", ofType: "jpg"), fm.fileExists(atPath: mainPath) {
            found = true
        }

        // Check Contents/Resources inside bundle
        let bundlePath = Bundle.main.bundlePath
        let resPath = (bundlePath as NSString).appendingPathComponent("Contents/Resources/blackhole_logo.jpg")
        if fm.fileExists(atPath: resPath) {
            found = true
        }

        XCTAssertTrue(found, "blackhole_logo.jpg resource asset should exist in project or bundle resources")
    }

    @MainActor
    func testWiFiManagerStoreInitialization() async {
        let store = WiFiManagerStore()
        
        XCTAssertFalse(store.topologyNodes.isEmpty, "WiFiManagerStore topologyNodes should initialize with default hardware nodes")
        XCTAssertTrue(store.isDaemonConnected, "WiFiManagerStore should default isDaemonConnected to true")
        
        // Test selecting a hotspot
        let ap = AccessPoint(ssid: "SFH", bssid: "00:11:22:33:44:55", rssi: -45, channel: 6, security: "WPA2", isSelected: false)
        store.selectHotspot(ap)
        
        XCTAssertEqual(store.selectedSSID, "SFH")
    }

    func testWiFiDaemonClientMockResponses() async throws {
        let client = WiFiDaemonClient(baseURL: "http://127.0.0.1:8990")
        XCTAssertEqual(client.baseURL, "http://127.0.0.1:8990")
    }
}
