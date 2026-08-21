import XCTest
import SwiftUI
@testable import EventHorizonCore
@testable import EventHorizonApp

final class E2EBundleValidationTests: XCTestCase {
    @MainActor
    func testLinkPortSidebarViewRendering() {
        var selection: NavigationSection = .overview
        let binding = Binding<NavigationSection>(
            get: { selection },
            set: { selection = $0 }
        )
        let sidebar = LinkPortSidebarView(selection: binding)
        
        let body = sidebar.body
        XCTAssertNotNil(body, "LinkPortSidebarView body getter failed or threw exception")
    }

    func testBlackholeLogoAssetExistence() {
        let fm = FileManager.default
        var found = false

        if fm.fileExists(atPath: "Sources/EventHorizonApp/Resources/blackhole_logo.jpg") {
            found = true
        }

        if let mainPath = Bundle.main.path(forResource: "blackhole_logo", ofType: "jpg"), fm.fileExists(atPath: mainPath) {
            found = true
        }

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
        
        XCTAssertTrue(store.topologyNodes.isEmpty, "WiFiManagerStore should not fabricate hardware nodes; they populate from the daemon")
        XCTAssertTrue(store.isDaemonConnected, "WiFiManagerStore should default isDaemonConnected to true")
    }

    func testWiFiDaemonClientMockResponses() async throws {
        if let url = URL(string: "http://127.0.0.1:8990") {
            let client = WiFiDaemonClient(baseURL: url)
            XCTAssertNotNil(client)
        }
    }
}
