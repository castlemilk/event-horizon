import Foundation

public protocol RuntimeSupervising: Sendable {
    func ensureDaemonRunning() async throws
    func installDaemonService() async throws
    func restartDaemonService() async throws
}

public enum SupervisorError: LocalizedError {
    case daemonWouldNotStart(String)

    public var errorDescription: String? {
        switch self {
        case .daemonWouldNotStart(let path):
            return "The usbwifi daemon at \(path) would not start, privileged or otherwise."
        }
    }
}

public actor RuntimeSupervisor: RuntimeSupervising {
    private var process: Process?

    public init() {}

    public func ensureDaemonRunning() async throws {
        if let proc = self.process, proc.isRunning {
            if await isDaemonReachable() {
                return
            }
        }
        if await isDaemonReachable() {
            return
        }

        // 2. Resolve daemon executable path (bundle resource, MacOS directory, or relative bin)
        guard let binaryURL = resolveDaemonBinary() else {
            print("[SUPERVISOR] Warning: Could not locate bundled usbwifi binary. Using standalone client mode.")
            return
        }

        // The daemon needs root: it claims the USB dongle through libusb
        // and creates the utun interface. Spawning it unprivileged
        // produced a process that started, failed both, and left the app
        // reporting success with nothing serving :8990.
        //
        // /etc/sudoers.d/usbwifi permits this binary passwordlessly, so
        // `sudo -n` works with no prompt and no TTY. If that rule is
        // absent the spawn is retried unprivileged, which is enough for
        // the read-only API, and the limitation is stated rather than
        // hidden.
        print("[SUPERVISOR] Spawning usbwifi background process: \(binaryURL.path)...")

        if try await spawn(binaryURL, privileged: true) {
            print("[SUPERVISOR] usbwifi daemon started successfully (privileged).")
            return
        }

        print("[SUPERVISOR] Privileged start failed — /etc/sudoers.d/usbwifi may be missing.")
        print("[SUPERVISOR] Retrying unprivileged: USB claiming and utun setup will not work.")

        if try await spawn(binaryURL, privileged: false) {
            print("[SUPERVISOR] usbwifi daemon started, but WITHOUT privileges.")
            print("[SUPERVISOR] Install the sudoers rule, or start it manually:")
            print("[SUPERVISOR]   sudo \(binaryURL.path) --port 8990")
            return
        }

        throw SupervisorError.daemonWouldNotStart(binaryURL.path)
    }

    /// Starts the daemon and waits for its API to answer.
    /// Returns false if it never became reachable.
    private func spawn(_ binaryURL: URL, privileged: Bool) async throws -> Bool {
        let proc = Process()
        if privileged {
            // -n so a missing sudoers rule fails immediately instead of
            // blocking on a password prompt the app cannot answer.
            proc.executableURL = URL(fileURLWithPath: "/usr/bin/sudo")
            proc.arguments = ["-n", binaryURL.path, "--port", "8990"]
        } else {
            proc.executableURL = binaryURL
            proc.arguments = ["--port", "8990"]
        }

        do {
            try proc.run()
        } catch {
            print("[SUPERVISOR] Could not launch: \(error.localizedDescription)")
            return false
        }
        self.process = proc

        // The daemon scans the radio and brings up utun before it
        // listens, which takes longer than the previous 5s allowance —
        // so the app concluded it had failed while it was still starting.
        for _ in 0..<60 {
            if await isDaemonReachable() {
                return true
            }
            if !proc.isRunning {
                print("[SUPERVISOR] Daemon exited during startup (status \(proc.terminationStatus)).")
                return false
            }
            try await Task.sleep(for: .milliseconds(500))
        }
        return false
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
        let resDirBin = Bundle.main.bundleURL.appendingPathComponent("Contents/Resources/usbwifi")
        if FileManager.default.fileExists(atPath: resDirBin.path) {
            return resDirBin
        }

        let macosDirBin = Bundle.main.bundleURL.appendingPathComponent("Contents/MacOS/usbwifi")
        if FileManager.default.fileExists(atPath: macosDirBin.path) {
            return macosDirBin
        }

        if let bundleResource = Bundle.main.url(forResource: "usbwifi", withExtension: nil) {
            return bundleResource
        }

        if let resourceDir = Bundle.main.resourceURL?.appendingPathComponent("usbwifi"),
           FileManager.default.fileExists(atPath: resourceDir.path) {
            return resourceDir
        }

        let cwdBin = URL(fileURLWithPath: FileManager.default.currentDirectoryPath).appendingPathComponent("bin/usbwifi")
        if FileManager.default.fileExists(atPath: cwdBin.path) {
            return cwdBin
        }

        let devBin = URL(fileURLWithPath: "/Users/benebsworth/projects/event-horizon/bin/usbwifi")
        if FileManager.default.fileExists(atPath: devBin.path) {
            return devBin
        }

        return nil
    }

    public func isLaunchDaemonInstalled() -> Bool {
        FileManager.default.fileExists(atPath: "/Library/LaunchDaemons/com.castlemilk.eventhorizon.usbwifi.plist")
    }

    public func installDaemonService() async throws {
        guard let binaryURL = resolveDaemonBinary() else {
            throw NSError(domain: "RuntimeSupervisor", code: 1, userInfo: [NSLocalizedDescriptionKey: "Bundled usbwifi binary not found in application bundle."])
        }

        let script = """
        mkdir -p "/Library/Application Support/EventHorizon"
        cp -f "\(binaryURL.path)" "/Library/Application Support/EventHorizon/usbwifi"
        chmod 755 "/Library/Application Support/EventHorizon/usbwifi"

        cat << 'PLISTEOF' > "/Library/LaunchDaemons/com.castlemilk.eventhorizon.usbwifi.plist"
        <?xml version="1.0" encoding="UTF-8"?>
        <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
        <plist version="1.0">
        <dict>
            <key>Label</key>
            <string>com.castlemilk.eventhorizon.usbwifi</string>
            <key>ProgramArguments</key>
            <array>
                <string>/Library/Application Support/EventHorizon/usbwifi</string>
                <string>--port</string>
                <string>8990</string>
            </array>
            <key>RunAtLoad</key>
            <true/>
            <key>KeepAlive</key>
            <true/>
            <key>StandardOutPath</key>
            <string>/var/log/usbwifi.log</string>
            <key>StandardErrorPath</key>
            <string>/var/log/usbwifi.err</string>
        </dict>
        </plist>
        PLISTEOF

        chmod 644 "/Library/LaunchDaemons/com.castlemilk.eventhorizon.usbwifi.plist"
        chown root:wheel "/Library/LaunchDaemons/com.castlemilk.eventhorizon.usbwifi.plist"
        launchctl bootout system /Library/LaunchDaemons/com.castlemilk.eventhorizon.usbwifi.plist 2>/dev/null || true
        launchctl bootstrap system /Library/LaunchDaemons/com.castlemilk.eventhorizon.usbwifi.plist
        """

        let escapedScript = script.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"")
        let appleScript = "do shell script \"\(escapedScript)\" with administrator privileges"

        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
        proc.arguments = ["-e", appleScript]
        try proc.run()
        proc.waitUntilExit()

        // Wait up to 5 seconds for daemon to respond
        for _ in 0..<25 {
            if await isDaemonReachable() {
                return
            }
            try await Task.sleep(for: .milliseconds(200))
        }
    }

    public func restartDaemonService() async throws {
        if let proc = self.process, proc.isRunning {
            proc.terminate()
            self.process = nil
        }
        try await ensureDaemonRunning()
    }

    deinit {
        process?.terminate()
    }
}
