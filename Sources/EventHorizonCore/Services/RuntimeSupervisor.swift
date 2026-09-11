import CryptoKit
import Foundation
import os

/// Supervisor output goes to the unified log, not stdout.
///
/// These were print() calls, which is invisible for a GUI app: launched from
/// Finder there is no terminal attached, and print() does not reach the
/// unified log either. So when the daemon failed to start there was no way to
/// find out why — `log show` returned nothing and running the bundle
/// executable by hand produced no output because the SwiftUI .task that calls
/// bootstrap() only fires once a window appears.
let supervisorLog = SupervisorLog()

/// Writes to the unified log AND to ~/Library/Logs/EventHorizon/supervisor.log.
///
/// The file is the point. These messages were print() calls, invisible for an
/// app launched from Finder; switching to os_log did not help either — no
/// entries reached the log store, so the reason the daemon would not start
/// stayed unknowable from outside the process. A plain file always works, and
/// startup diagnostics are exactly what someone needs when the app looks alive
/// and does nothing.
public struct SupervisorLog {
    private let logger = Logger(subsystem: "com.castlemilk.eventhorizon", category: "supervisor")

    public func notice(_ message: String) {
        logger.notice("\(message, privacy: .public)")
        Self.append(message)
    }

    static let fileURL: URL = {
        let dir = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Logs/EventHorizon", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir.appendingPathComponent("supervisor.log")
    }()

    private static let queue = DispatchQueue(label: "com.castlemilk.eventhorizon.supervisorlog")

    private static func append(_ message: String) {
        queue.async {
            let line = "\(ISO8601DateFormatter().string(from: Date()))  \(message)\n"
            guard let data = line.data(using: .utf8) else { return }
            if let h = try? FileHandle(forWritingTo: fileURL) {
                defer { try? h.close() }
                _ = try? h.seekToEnd()
                try? h.write(contentsOf: data)
            } else {
                try? data.write(to: fileURL)
            }
        }
    }
}

public protocol RuntimeSupervising: Sendable {
    func ensureDaemonRunning() async throws
    func installDaemonService() async throws
    func restartDaemonService() async throws

    /// Why the last privileged start did not happen, if it did not.
    ///
    /// This is on the protocol because callers need to tell a declined
    /// authorization from a broken daemon: one should stop the app retrying and
    /// say so, the other is worth retrying. The default returns nil, so an
    /// implementation that never escalates need not care.
    func privilegeError() async -> String?
}

public extension RuntimeSupervising {
    func privilegeError() async -> String? { nil }
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
        supervisorLog.notice("ensureDaemonRunning: entered")

        // Reachable is not sufficient. A daemon left over from an older bundle
        // answers :8990 exactly like ours, so "something is listening" was
        // taken as "we are running" and a freshly built app went on serving
        // from a stale binary — every fix present in the bundle, none of them
        // in the process. Insist it is OUR daemon.
        if await isDaemonReachable() {
            if await daemonIsOurs() {
                return
            }
            let why = await daemonMismatchReason() ?? "the running daemon is not this app's"
            supervisorLog.notice("replacing a foreign daemon: \(why)")
            try await stopRunningDaemon()
        }

        // If the service is installed, launchd owns the daemon's lifetime and
        // KeepAlive brings it back on its own. Spawning a second copy here
        // would fight it for the USB device and would ask for an authorization
        // that the install already granted, so wait briefly for launchd instead
        // of starting a rival.
        if Self.launchDaemonInstalled() {
            supervisorLog.notice("LaunchDaemon is installed; waiting for launchd rather than spawning a second daemon")
            for _ in 0..<10 {
                if await isDaemonReachable() {
                    return
                }
                try? await Task.sleep(for: .milliseconds(300))
            }
            lastPrivilegeError = nil
            throw SupervisorError.daemonWouldNotStart(Self.launchDaemonPlist)
        }

        // 2. Resolve daemon executable path (bundle resource, MacOS directory, or relative bin)
        guard let binaryURL = resolveDaemonBinary() else {
            supervisorLog.notice("Warning: Could not locate bundled usbwifi binary. Using standalone client mode.")
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
        supervisorLog.notice("Spawning usbwifi background process: \(binaryURL.path)...")

        do {
            let grant = try PrivilegedLauncher.run(
                executable: binaryURL.path,
                arguments: ["--port", "8990"],
                detached: true
            )
            switch grant {
            case .passwordless:
                supervisorLog.notice("Started privileged via the existing sudoers rule.")
            case .authorized:
                supervisorLog.notice("Started privileged after authorization.")
            }
            if await waitForDaemon() {
                return
            }
            supervisorLog.notice("Authorized, but the daemon never answered on :8990.")
            throw SupervisorError.daemonWouldNotStart(binaryURL.path)
        } catch let err as PrivilegedLauncher.LaunchError {
            // Declining is a decision, not a crash. Report it as such and
            // leave the app in client mode rather than pretending to run.
            supervisorLog.notice("\(err.localizedDescription)")
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

    /// Path of the LaunchDaemon `installDaemonService()` writes.
    static let launchDaemonPlist = "/Library/LaunchDaemons/com.castlemilk.eventhorizon.usbwifi.plist"

    /// Whether the daemon is installed as a system service.
    ///
    /// This matters because the two ways of running the daemon are not
    /// equivalent. An ad-hoc privileged spawn needs an authorization every
    /// single time the daemon is not already up — the passwordless sudoers rule
    /// people add covers `bin/usbwifi` in a checkout, not the copy inside the
    /// app bundle, so `sudo -n` misses and the GUI prompt appears again and
    /// again. The LaunchDaemon is authorized once at install and then owned by
    /// launchd, which has KeepAlive set and restarts it without asking anyone.
    ///
    /// So when the service is installed, the app should get out of the way.
    static func launchDaemonInstalled() -> Bool {
        FileManager.default.fileExists(atPath: launchDaemonPlist)
    }

    private func isDaemonReachable() async -> Bool {
        await daemonIdentity() != nil
    }

    /// What the daemon on :8990 says it is: its build fingerprint and where it
    /// was launched from. nil when nothing answers.
    private func daemonIdentity() async -> (fingerprint: String, path: String)? {
        guard let url = URL(string: "http://127.0.0.1:8990/api/status") else { return nil }
        do {
            let (data, resp) = try await URLSession.shared.data(from: url)
            guard (resp as? HTTPURLResponse)?.statusCode == 200 else { return nil }
            guard
                let root = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                let payload = root["data"] as? [String: Any]
            else { return ("", "") }
            return (
                payload["buildFingerprint"] as? String ?? "",
                payload["executablePath"] as? String ?? ""
            )
        } catch {
            return nil
        }
    }

    /// SHA-256 of the daemon binary inside this app bundle.
    ///
    /// This is the other half of the identity check: the app knows what its own
    /// daemon should hash to, so it can tell "my daemon is running" from
    /// "something is running".
    private func bundledDaemonFingerprint() -> String? {
        guard let url = resolveDaemonBinary(),
              let data = FileManager.default.contents(atPath: url.path)
        else { return nil }
        return SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
    }

    /// Whether the daemon currently answering is the one this app ships.
    ///
    /// A stale daemon from an older bundle answers :8990 perfectly well, which
    /// is how a rebuilt app can spend an afternoon appearing to have no effect:
    /// every fix is in the bundle, and the process serving requests predates
    /// all of them.
    ///
    /// A daemon reporting NO fingerprint is not "unknown", it is old — every
    /// build that carries this check reports one, so its absence dates the
    /// process to before the check existed. That is exactly the stale daemon
    /// worth replacing, and an earlier version of this treated it as a match,
    /// which would have left the commonest case unhandled.
    ///
    /// The one genuine cannot-tell is our own side: if the bundled binary
    /// cannot be hashed we have nothing to compare against, and tearing down a
    /// working daemon on that basis would be worse than the problem.
    public func daemonIsOurs() async -> Bool {
        guard let running = await daemonIdentity() else { return false }
        guard let ours = bundledDaemonFingerprint(), !ours.isEmpty else { return true }
        return running.fingerprint == ours
    }

    /// Describes a mismatch for the UI, or nil when the daemon is ours.
    public func daemonMismatchReason() async -> String? {
        guard let running = await daemonIdentity() else { return nil }
        guard let ours = bundledDaemonFingerprint(), !ours.isEmpty else { return nil }
        if running.fingerprint == ours { return nil }
        if running.fingerprint.isEmpty {
            return "the daemon on :8990 predates this app's build and reports no identity"
        }
        let where_ = running.path.isEmpty ? "an unknown location" : running.path
        return "a daemon from \(where_) is running, not the one in this app"
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

    /// Stops whatever daemon is on :8990 using its own --stop, privileged.
    ///
    /// Through the daemon rather than a signal, so it tears down utun and
    /// releases the USB claim instead of leaving an interface behind that
    /// outlives the radio it was bridging.
    private func stopRunningDaemon() async throws {
        guard let binaryURL = resolveDaemonBinary() else { return }
        do {
            _ = try PrivilegedLauncher.run(
                executable: binaryURL.path, arguments: ["--stop"], detached: false)
        } catch {
            supervisorLog.notice("daemon --stop failed: \(error.localizedDescription)")
        }
        for _ in 0..<20 {
            if await !isDaemonReachable() { return }
            try? await Task.sleep(for: .milliseconds(250))
        }
    }

    public func restartDaemonService() async throws {
        // Terminating self.process is not enough and was never enough. A
        // privileged daemon is spawned DETACHED through osascript, so
        // self.process is the osascript wrapper, which has long since exited —
        // the daemon it started is a root process this app never held a handle
        // to. "Restart Daemon" therefore terminated nothing, called
        // ensureDaemonRunning, found the old daemon still answering on :8990,
        // and returned success having changed nothing. Deploying a fixed binary
        // that way is impossible, which is exactly when a restart is wanted.
        if let proc = self.process, proc.isRunning {
            proc.terminate()
            self.process = nil
        }

        supervisorLog.notice("stopping the running daemon via --stop before restarting")
        try await stopRunningDaemon()
        try await ensureDaemonRunning()
    }

    deinit {
        process?.terminate()
    }
}
