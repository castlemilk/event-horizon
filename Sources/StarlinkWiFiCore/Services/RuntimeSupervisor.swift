import Foundation

public protocol RuntimeSupervising: Sendable {
    func ensureDaemonRunning() async throws
}

public actor RuntimeSupervisor: RuntimeSupervising {
    private var process: Process?

    public init() {}

    public func ensureDaemonRunning() async throws {
        // 1. Check if daemon API port 8990 is already responsive
        if await isDaemonReachable() {
            print("[SUPERVISOR] Go usbwifi daemon is already running and reachable.")
            return
        }

        // 2. Resolve daemon executable path (bundle resource, MacOS directory, or relative bin)
        guard let binaryURL = resolveDaemonBinary() else {
            print("[SUPERVISOR] Warning: Could not locate bundled usbwifi binary. Using standalone client mode.")
            return
        }

        print("[SUPERVISOR] Spawning usbwifi background process: \(binaryURL.path)...")
        let proc = Process()
        proc.executableURL = binaryURL
        proc.arguments = ["--ssid", "aliens exist", "--port", "8990"]

        try proc.run()
        self.process = proc

        // Wait up to 5 seconds for HTTP API to wake up
        for _ in 0..<25 {
            if await isDaemonReachable() {
                print("[SUPERVISOR] usbwifi daemon started successfully!")
                return
            }
            try await Task.sleep(for: .milliseconds(200))
        }
    }

    private func isDaemonReachable() async -> Bool {
        guard let url = URL(string: "http://127.0.0.1:8990/api/status") else { return false }
        do {
            let (_, resp) = try await URLSession.shared.data(from: url)
            return (resp as? HTTPURLResponse)?.statusCode == 200
        } catch {
            return false
        }
    }

    private func resolveDaemonBinary() -> URL? {
        if let bundleResource = Bundle.main.url(forResource: "usbwifi", withExtension: nil) {
            return bundleResource
        }

        let macosDirBin = Bundle.main.bundleURL.appendingPathComponent("Contents/MacOS/usbwifi")
        if FileManager.default.fileExists(atPath: macosDirBin.path) {
            return macosDirBin
        }

        if let resourceDir = Bundle.main.resourceURL?.appendingPathComponent("usbwifi"),
           FileManager.default.fileExists(atPath: resourceDir.path) {
            return resourceDir
        }

        let cwdBin = URL(fileURLWithPath: FileManager.default.currentDirectoryPath).appendingPathComponent("bin/usbwifi")
        if FileManager.default.fileExists(atPath: cwdBin.path) {
            return cwdBin
        }

        let projBin = URL(fileURLWithPath: "/Users/benebsworth/projects/starlink-sdk/bin/usbwifi")
        if FileManager.default.fileExists(atPath: projBin.path) {
            return projBin
        }

        return nil
    }

    deinit {
        process?.terminate()
    }
}
