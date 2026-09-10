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

    /// Why the last privileged start did not happen, for the UI to show.
    /// Declining the prompt is a legitimate choice and the app should say so
    /// rather than appearing broken.
    public private(set) var lastPrivilegeError: String?

    public init() {}

    public func privilegeError() -> String? { lastPrivilegeError }

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

        // The daemon needs root: it claims the USB dongle through libusb and
        // brings up the utun. There is no useful unprivileged mode — a daemon
        // that can do neither is a daemon that cannot see a dongle.
        //
        // So: try the passwordless sudoers rule first (silent on a developer
        // box), and otherwise ASK, with the standard macOS authorization
        // panel. The app used to fall back to starting it unprivileged and
        // print advice about editing /etc/sudoers.d — which meant a normal
        // user saw an app that launched, looked fine, and never worked, with
        // no way to grant the permission it actually needed.
        print("[SUPERVISOR] Spawning usbwifi background process: \(binaryURL.path)...")

        do {
            let grant = try PrivilegedLauncher.run(
                executable: binaryURL.path,
                arguments: ["--port", "8990"],
                detached: true
            )
            switch grant {
            case .passwordless:
                print("[SUPERVISOR] Started privileged via the existing sudoers rule.")
            case .authorized:
                print("[SUPERVISOR] Started privileged after authorization.")
            }
            if await waitForDaemon() {
                return
            }
            print("[SUPERVISOR] Authorized, but the daemon never answered on :8990.")
            throw SupervisorError.daemonWouldNotStart(binaryURL.path)
        } catch let err as PrivilegedLauncher.LaunchError {
            // Declining is a decision, not a crash. Report it as such and
            // leave the app in client mode rather than pretending to run.
            print("[SUPERVISOR] \(err.localizedDescription)")
            lastPrivilegeError = err.localizedDescription
            throw err
        }
    }

    /// Polls the API until the daemon answers. It scans the radio before it
    /// listens, so this allows well past a naive few seconds — the app used to
    /// conclude failure while the daemon was still starting.
    private func waitForDaemon() async -> Bool {
        for _ in 0..<60 {
            if await isDaemonReachable() { return true }
            try? await Task.sleep(for: .milliseconds(500))
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

        // A developer checkout, and only in a debug build. This used to be a
        // hardcoded absolute home directory compiled into release builds too —
        // a path that exists on exactly one machine. Anywhere else it just
        // returns nil, so a shipped app would report no daemon while its last
        // resort was looking inside someone else's home directory.
        #if DEBUG
        let devBin = URL(fileURLWithPath: NSHomeDirectory())
            .appendingPathComponent("projects/event-horizon/bin/usbwifi")
        if FileManager.default.fileExists(atPath: devBin.path) {
            return devBin
        }
        #endif

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
