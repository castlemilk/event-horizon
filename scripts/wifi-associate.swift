import CoreWLAN
import Foundation

// Real Wi-Fi association helper for the Event Horizon daemon.
// Run via the Swift interpreter (`swift scripts/wifi-associate.swift <ssid> <password>`)
// so CoreWLAN inherits the invoking terminal's TCC/Location permission.
//
// Exit codes:
//   0  CONNECTED <ssid>
//   4  network not found in scan
//   5  associate succeeded but SSID not confirmed
//   6  scan/associate error

func main() -> Int32 {
    let args = CommandLine.arguments
    guard args.count >= 3 else {
        FileHandle.standardError.write(Data("usage: swift wifi-associate.swift <ssid> <password>\n".utf8))
        return 2
    }
    let ssid = args[1]
    let password = args[2]
    let client = CWWiFiClient.shared()
    guard let iface = client.interface() else {
        FileHandle.standardError.write(Data("no wifi interface\n".utf8))
        return 3
    }
    do {
        let scan = try iface.scanForNetworks(withName: nil)
        guard let net = scan.first(where: { $0.ssid == ssid }) else {
            let names = scan.compactMap { $0.ssid }.sorted().joined(separator: ", ")
            FileHandle.standardError.write(Data("network \(ssid) not found; visible: \(names)\n".utf8))
            return 4
        }
        try iface.associate(to: net, password: password)
        Thread.sleep(forTimeInterval: 4)
        let current = iface.ssid()
        if current == ssid {
            print("CONNECTED \(ssid)")
            return 0
        }
        FileHandle.standardError.write(Data("current=\(current ?? "nil")\n".utf8))
        return 5
    } catch {
        let code = (error as NSError).code
        FileHandle.standardError.write(Data("scan/assoc error \(code): \(error)\n".utf8))
        return 6
    }
}

exit(main())
