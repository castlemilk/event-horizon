import Foundation

/// Runs a command as root, showing the standard macOS authorization prompt
/// when it needs to.
///
/// The daemon needs root for two unavoidable reasons: libusb has to claim the
/// dongle's interfaces, and bringing the link up creates a utun and installs a
/// route. Before this existed the app tried `sudo -n` and, when that failed,
/// started the daemon *unprivileged* — which cannot do either, so the app came
/// up looking healthy and no dongle ever worked. A user with no sudoers rule
/// had no way to grant permission and no clear indication that permission was
/// the problem.
///
/// Order matters here. `sudo -n` is tried first because it is silent: on a
/// machine that already has the rule (any developer box) nothing pops up. Only
/// when that fails do we ask, and then the user gets the familiar system
/// dialog rather than an instruction to go and edit /etc/sudoers.d by hand.
public enum PrivilegedLauncher {

    public enum LaunchError: LocalizedError {
        case cancelled
        case failed(status: Int32, message: String)

        public var errorDescription: String? {
            switch self {
            case .cancelled:
                return "Authorization was declined, so the dongle cannot be claimed."
            case let .failed(status, message):
                let detail = message.trimmingCharacters(in: .whitespacesAndNewlines)
                return detail.isEmpty
                    ? "The privileged command exited with status \(status)."
                    : detail
            }
        }
    }

    /// How the command was ultimately started, so callers can tell the user
    /// whether they were asked for a password.
    public enum Grant {
        case passwordless   // an existing sudoers rule covered it
        case authorized     // the user approved the system prompt
    }

    /// Runs `executable` with `arguments` as root.
    ///
    /// When `detached` is true the process is left running in the background
    /// and this returns as soon as it has been started — which is what a
    /// long-lived daemon needs, since the authorization prompt otherwise
    /// blocks until the command exits.
    @discardableResult
    public static func run(
        executable: String,
        arguments: [String],
        detached: Bool
    ) throws -> Grant {
        if runPasswordlessSudo(executable: executable, arguments: arguments, detached: detached) {
            return .passwordless
        }
        try runWithAuthorizationPrompt(executable: executable, arguments: arguments, detached: detached)
        return .authorized
    }

    // MARK: - sudo -n

    /// Tries the non-interactive path. `-n` makes a missing rule fail at once
    /// rather than blocking on a password prompt that a GUI app cannot answer.
    private static func runPasswordlessSudo(
        executable: String,
        arguments: [String],
        detached: Bool
    ) -> Bool {
        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: "/usr/bin/sudo")
        proc.arguments = ["-n", executable] + arguments
        proc.standardOutput = FileHandle.nullDevice
        proc.standardError = FileHandle.nullDevice
        do {
            try proc.run()
        } catch {
            return false
        }
        if detached {
            // Give it a moment to fail on a missing sudoers rule; sudo exits
            // ~immediately with status 1 in that case. Surviving this window
            // means it is genuinely running.
            Thread.sleep(forTimeInterval: 0.4)
            if !proc.isRunning && proc.terminationStatus != 0 {
                return false
            }
            return true
        }
        proc.waitUntilExit()
        return proc.terminationStatus == 0
    }

    // MARK: - authorization prompt

    /// Shows the system authorization dialog and runs the command as root.
    ///
    /// This uses `do shell script … with administrator privileges`, which is
    /// what produces the standard "… wants to make changes" panel. It is not
    /// App Store material, but neither is this app: raw libusb access cannot
    /// happen inside the App Sandbox, so the sandboxed build could never claim
    /// a dongle regardless.
    private static func runWithAuthorizationPrompt(
        executable: String,
        arguments: [String],
        detached: Bool
    ) throws {
        var shell = ([executable] + arguments).map(shellQuoted).joined(separator: " ")
        if detached {
            // do shell script waits for completion, so a daemon has to be
            // fully detached or the prompt would never return.
            shell = "nohup \(shell) >/dev/null 2>&1 & echo started"
        }

        let script = "do shell script \(appleScriptQuoted(shell)) with administrator privileges"

        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
        proc.arguments = ["-e", script]
        let errPipe = Pipe()
        proc.standardOutput = FileHandle.nullDevice
        proc.standardError = errPipe

        try proc.run()
        let errData = errPipe.fileHandleForReading.readDataToEndOfFile()
        proc.waitUntilExit()

        guard proc.terminationStatus != 0 else { return }

        let message = String(data: errData, encoding: .utf8) ?? ""
        // -128 is the documented "user cancelled" code from AppleScript, and
        // it is worth distinguishing: declining is a choice, not a failure.
        if message.contains("-128") || message.localizedCaseInsensitiveContains("User canceled") {
            throw LaunchError.cancelled
        }
        throw LaunchError.failed(status: proc.terminationStatus, message: message)
    }

    // MARK: - quoting

    /// Single-quotes a shell argument. The bundle path contains a space
    /// ("Event Horizon.app"), so unquoted interpolation would split it.
    static func shellQuoted(_ s: String) -> String {
        "'" + s.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    /// Wraps a string as an AppleScript literal. The command is already
    /// shell-quoted, so this only has to survive AppleScript's own parser —
    /// backslashes first, then double quotes, or the escaping eats itself.
    static func appleScriptQuoted(_ s: String) -> String {
        let escaped = s
            .replacingOccurrences(of: "\\", with: "\\\\")
            .replacingOccurrences(of: "\"", with: "\\\"")
        return "\"\(escaped)\""
    }
}
