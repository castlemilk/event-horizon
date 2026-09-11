// EventHorizon dext activator.
//
// macOS has no CLI to activate a DriverKit system extension: activation must
// come from an app bundle that calls OSSystemExtensionRequest and is signed
// with com.apple.developer.system-extension.install. This tiny bundle embeds
// AIC8800D80Driver.dext at Contents/Library/SystemExtensions/ and drives the
// activation/deactivation request, printing every delegate callback so the
// flow is visible from the terminal.
//
//   EventHorizonActivator.app/Contents/MacOS/EventHorizonActivator [activate|deactivate|status]

import Foundation
import SystemExtensions

let dextID = "com.eventhorizon.driver.AIC8800D80"

final class Delegate: NSObject, OSSystemExtensionRequestDelegate {
    func request(_ request: OSSystemExtensionRequest,
                 actionForReplacingExtension existing: OSSystemExtensionProperties,
                 withExtension ext: OSSystemExtensionProperties) -> OSSystemExtensionRequest.ReplacementAction {
        FileHandle.standardError.write(
            "replacing installed \(existing.bundleShortVersion) (\(existing.bundleVersion)) with \(ext.bundleShortVersion) (\(ext.bundleVersion))\n".data(using: .utf8)!)
        return .replace
    }

    func requestNeedsUserApproval(_ request: OSSystemExtensionRequest) {
        print("awaiting user approval — open System Settings > General > Login Items & Extensions > Driver Extensions and enable it")
    }

    func request(_ request: OSSystemExtensionRequest,
                 didFinishWithResult result: OSSystemExtensionRequest.Result) {
        switch result {
        case .completed:
            print("request completed")
        case .willCompleteAfterReboot:
            print("request will complete after reboot")
        @unknown default:
            print("request finished: \(result.rawValue)")
        }
        exit(0)
    }

    func request(_ request: OSSystemExtensionRequest, didFailWithError error: Error) {
        let ns = error as NSError
        FileHandle.standardError.write(
            "request FAILED: \(ns.domain) code \(ns.code): \(ns.localizedDescription)\n".data(using: .utf8)!)
        exit(1)
    }
}

let mode = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "activate"
let delegate = Delegate()

// Diagnostics: show where the framework will look for the embedded extension.
let sysExtDir = Bundle.main.bundleURL.appendingPathComponent("Contents/Library/SystemExtensions")
let found = (try? FileManager.default.contentsOfDirectory(atPath: sysExtDir.path)) ?? []
FileHandle.standardError.write("main bundle: \(Bundle.main.bundlePath)\n".data(using: .utf8)!)
FileHandle.standardError.write("SystemExtensions/: \(found)\n".data(using: .utf8)!)

let request: OSSystemExtensionRequest
switch mode {
case "deactivate":
    request = OSSystemExtensionRequest.deactivationRequest(forExtensionWithIdentifier: dextID, queue: .main)
case "activate":
    request = OSSystemExtensionRequest.activationRequest(forExtensionWithIdentifier: dextID, queue: .main)
default:
    FileHandle.standardError.write("usage: EventHorizonActivator [activate|deactivate]\n".data(using: .utf8)!)
    exit(2)
}

request.delegate = delegate
OSSystemExtensionManager.shared.submitRequest(request)
print("submitted \(mode) request for \(dextID); waiting for callbacks…")
RunLoop.main.run()
