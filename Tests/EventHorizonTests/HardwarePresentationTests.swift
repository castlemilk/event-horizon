import XCTest
@testable import EventHorizonCore

final class HardwarePresentationTests: XCTestCase {
    private func node(interface: String = "en10", target: String = "", ip: String = "", status: String = "Inactive") -> HardwareTopologyNode {
        HardwareTopologyNode(usbDriver: "USB Wi-Fi Adapter", vendorId: "0x1234", productId: "0x5678", serialNumber: "", speed: "", bsdInterface: interface, networkTarget: target, ipAddress: ip, subnetMask: "", gateway: "", macAddress: "", status: status, driverType: "")
    }

    func testHyphenatedWiFiIsRecognized() {
        XCTAssertEqual(node().category, .usbWiFiDongle)
    }

    func testTargetLabelDoesNotProveConnection() {
        XCTAssertNotEqual(node(target: "Not Connected").routeBadge, "CONNECTED")
        XCTAssertNotEqual(node(target: "Preferred network").routeBadge, "CONNECTED")
    }

    func testConnectedAdapterRequiresObservedAddress() {
        XCTAssertEqual(node(target: "Guest", ip: "192.0.2.2", status: "Connected").routeBadge, "CONNECTED")
        XCTAssertNotEqual(node(target: "Guest", status: "Disconnected").routeBadge, "CONNECTED")
    }
}
