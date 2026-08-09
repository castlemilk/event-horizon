import XCTest
@testable import StarlinkWiFiCore

final class StarlinkWiFiTests: XCTestCase {
    func testAccessPointModel() {
        let ap = AccessPoint(
            ssid: "CNH Starlink",
            bssid: "00:13:02:8f:9a:11",
            rssi: -50,
            channel: 6,
            security: "WPA2-PSK",
            isSelected: true
        )
        XCTAssertEqual(ap.ssid, "CNH Starlink")
        XCTAssertEqual(ap.rssi, -50)
        XCTAssertTrue(ap.isSelected)
    }

    func testHardwareTopologyModel() {
        let node = HardwareTopologyNode(
            usbDriver: "AICSemi AIC8800 USB Wi-Fi Dongle",
            vendorId: "0xA69C",
            productId: "0x8D80",
            serialNumber: "0113000001",
            speed: "USB 2.0 High-Speed (480 Mbps)",
            bsdInterface: "utun4",
            networkTarget: "CNH Starlink",
            ipAddress: "192.168.1.105",
            subnetMask: "255.255.255.0",
            gateway: "192.168.1.1",
            macAddress: "d0:c1:b5:21:d5:92",
            status: "Connected (WPA2-PSK)",
            driverType: "User-Space Wi-Fi Daemon (libusb)"
        )
        XCTAssertEqual(node.usbDriver, "AICSemi AIC8800 USB Wi-Fi Dongle")
        XCTAssertEqual(node.bsdInterface, "utun4")
        XCTAssertEqual(node.ipAddress, "192.168.1.105")
    }

    func testInterfaceStatModel() {
        let stat = InterfaceStat(
            name: "utun4",
            bytesIn: 1048576,
            bytesOut: 524288,
            packetsIn: 1000,
            packetsOut: 500,
            errorsIn: 0,
            errorsOut: 0,
            isUp: true,
            rxRateKBps: 312.5,
            txRateKBps: 68.0
        )
        XCTAssertEqual(stat.name, "utun4")
        XCTAssertEqual(stat.rxRateKBps, 312.5)
        XCTAssertTrue(stat.isUp)
    }

    func testPingResultModel() {
        let ping = PingResult(
            target: "1.1.1.1",
            isReachable: true,
            rttMs: 12,
            packetLossPercent: 0.0
        )
        XCTAssertEqual(ping.target, "1.1.1.1")
        XCTAssertTrue(ping.isReachable)
        XCTAssertEqual(ping.rttMs, 12)
    }

    func testStabilityStatsModel() {
        let stab = StabilityStats(
            uptimeSeconds: 300,
            uptimeFormatted: "00h 05m 00s",
            disconnectCount: 0,
            reconnectCount: 0,
            stabilityScore: 100.0,
            currentStatus: "CONNECTED"
        )
        XCTAssertEqual(stab.uptimeFormatted, "00h 05m 00s")
        XCTAssertEqual(stab.stabilityScore, 100.0)
    }
}
