import SwiftUI
import EventHorizonCore

public struct DriverInstallWizardSheet: View {
    @Bindable var store: WiFiManagerStore
    let defaultVID: UInt16
    let defaultPID: UInt16
    let defaultDeviceName: String
    let onDismiss: () -> Void

    @State private var useDriverKit = false
    @State private var autoScroll = true

    public init(
        store: WiFiManagerStore,
        vid: UInt16 = 0xa69c,
        pid: UInt16 = 0x8d81,
        deviceName: String = "UGREEN AX900 WiFi 6 (AIC8800D80)",
        onDismiss: @escaping () -> Void
    ) {
        self.store = store
        self.defaultVID = vid
        self.defaultPID = pid
        self.defaultDeviceName = deviceName
        self.onDismiss = onDismiss
    }

    private var progress: DriverInstallProgress? {
        store.installProgress
    }

    public var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack(spacing: 14) {
                ZStack {
                    Circle()
                        .fill(Color.blue.opacity(0.15))
                        .frame(width: 44, height: 44)
                    Image(systemName: "cpu.fill")
                        .font(.title3)
                        .foregroundStyle(.blue)
                }

                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 8) {
                        Text(progress?.deviceName ?? defaultDeviceName)
                            .font(.headline)
                        Text(String(format: "0x%04x:0x%04x", defaultVID, defaultPID))
                            .font(.caption2.monospaced())
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Color.secondary.opacity(0.12))
                            .clipShape(RoundedRectangle(cornerRadius: 4))
                    }
                    Text("Universal Driver Provisioning, Firmware Flashing & Virtual utun Network Setup")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Spacer()

                Button(action: onDismiss) {
                    Image(systemName: "xmark.circle.fill")
                        .font(.title2)
                        .foregroundStyle(.secondary.opacity(0.8))
                }
                .buttonStyle(.plain)
            }
            .padding(20)
            .background(Color(nsColor: .windowBackgroundColor))

            Divider()

            // Main Content Area
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    // Pipeline Progress Bar
                    VStack(alignment: .leading, spacing: 8) {
                        HStack {
                            Text("INSTALLATION PROGRESS")
                                .font(.caption2.weight(.bold))
                                .foregroundStyle(.secondary)
                            Spacer()
                            Text("\(progress?.percent ?? 0)%")
                                .font(.callout.weight(.bold).monospacedDigit())
                                .foregroundStyle(progress?.isSuccess == true ? .green : .blue)
                        }

                        GeometryReader { geo in
                            ZStack(alignment: .leading) {
                                RoundedRectangle(cornerRadius: 6)
                                    .fill(Color.secondary.opacity(0.15))
                                    .frame(height: 10)

                                RoundedRectangle(cornerRadius: 6)
                                    .fill(
                                        LinearGradient(
                                            colors: progress?.isSuccess == true ? [.green, .mint] : [.blue, .cyan],
                                            startPoint: .leading,
                                            endPoint: .trailing
                                        )
                                    )
                                    .frame(width: max(10, geo.size.width * CGFloat(progress?.percent ?? 0) / 100), height: 10)
                                    .animation(.easeInOut, value: progress?.percent)
                            }
                        }
                        .frame(height: 10)
                    }
                    .padding(16)
                    .background(Color(nsColor: .controlBackgroundColor))
                    .clipShape(RoundedRectangle(cornerRadius: 10))
                    .overlay(
                        RoundedRectangle(cornerRadius: 10)
                            .stroke(Color.secondary.opacity(0.1), lineWidth: 1)
                    )

                    // Stepper Milestones
                    VStack(alignment: .leading, spacing: 10) {
                        Text("PROVISIONING PIPELINE MILESTONES")
                            .font(.caption2.weight(.bold))
                            .foregroundStyle(.secondary)

                        VStack(spacing: 8) {
                            if let steps = progress?.steps, !steps.isEmpty {
                                ForEach(steps) { step in
                                    StepRowView(step: step)
                                }
                            } else {
                                ForEach(defaultPlaceholderSteps()) { step in
                                    StepRowView(step: step)
                                }
                            }
                        }
                    }

                    // Driver Strategy Selector
                    if progress == nil || !progress!.isActive {
                        VStack(alignment: .leading, spacing: 10) {
                            Text("DRIVER ARCHITECTURE STRATEGY")
                                .font(.caption2.weight(.bold))
                                .foregroundStyle(.secondary)

                            HStack(spacing: 12) {
                                ArchitectureOptionCard(
                                    title: "User-Space Stack (libusb + utun)",
                                    subtitle: "Recommended: Immediate activation, 0 reboot, high throughput",
                                    icon: "bolt.shield.fill",
                                    isSelected: !useDriverKit,
                                    badge: "INSTANT"
                                ) {
                                    useDriverKit = false
                                }

                                ArchitectureOptionCard(
                                    title: "Apple DriverKit Dext (.dext)",
                                    subtitle: "Native BSD interface (enX), requires System Settings approval",
                                    icon: "apple.logo",
                                    isSelected: useDriverKit,
                                    badge: "KERNEL"
                                ) {
                                    useDriverKit = true
                                }
                            }
                        }
                    }

                    // Live Console Terminal Log
                    VStack(alignment: .leading, spacing: 8) {
                        HStack {
                            Image(systemName: "terminal.fill")
                                .font(.caption)
                                .foregroundStyle(.green)
                            Text("HARDWARE & FIRMWARE STREAM LOGS")
                                .font(.caption2.weight(.bold))
                                .foregroundStyle(.secondary)
                            Spacer()
                            Text("\(progress?.logs.count ?? 0) events")
                                .font(.system(size: 9).monospaced())
                                .foregroundStyle(.secondary)
                        }

                        ScrollViewReader { proxy in
                            ScrollView {
                                VStack(alignment: .leading, spacing: 4) {
                                    if let logs = progress?.logs, !logs.isEmpty {
                                        ForEach(Array(logs.enumerated()), id: \.offset) { index, log in
                                            Text(log)
                                                .font(.system(size: 11).monospaced())
                                                .foregroundStyle(logColor(log))
                                                .id(index)
                                        }
                                    } else {
                                        Text("[READY] Awaiting driver provisioning trigger...")
                                            .font(.system(size: 11).monospaced())
                                            .foregroundStyle(.secondary)
                                    }
                                }
                                .padding(10)
                                .frame(maxWidth: .infinity, alignment: .leading)
                            }
                            .frame(height: 130)
                            .background(Color.black.opacity(0.9))
                            .clipShape(RoundedRectangle(cornerRadius: 8))
                            .overlay(
                                RoundedRectangle(cornerRadius: 8)
                                    .stroke(Color.secondary.opacity(0.2), lineWidth: 1)
                            )
                        }
                    }
                }
                .padding(20)
            }

            Divider()

            // Footer Actions
            HStack(spacing: 12) {
                if progress?.isSuccess == true {
                    Label("Driver Installed & Validated Successfully", systemImage: "checkmark.circle.fill")
                        .font(.callout.weight(.semibold))
                        .foregroundStyle(.green)
                } else if let err = progress?.error {
                    Label("Error: \(err)", systemImage: "exclamationmark.triangle.fill")
                        .font(.caption)
                        .foregroundStyle(.red)
                }

                Spacer()

                Button("Cancel / Close") {
                    onDismiss()
                }
                .buttonStyle(.plain)
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
                .background(Color.secondary.opacity(0.12))
                .clipShape(RoundedRectangle(cornerRadius: 6))

                if store.isInstallingDriver {
                    HStack(spacing: 8) {
                        ProgressView()
                            .scaleEffect(0.7)
                        Text("Flashing & Provisioning...")
                            .font(.body.weight(.semibold))
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 8)
                    .background(Color.blue.opacity(0.8))
                    .foregroundStyle(.white)
                    .clipShape(RoundedRectangle(cornerRadius: 6))
                } else {
                    Button(action: {
                        Task {
                            await store.startDriverInstallation(vid: defaultVID, pid: defaultPID, useDriverKit: useDriverKit)
                        }
                    }) {
                        HStack(spacing: 6) {
                            Image(systemName: progress?.isSuccess == true ? "arrow.clockwise" : "bolt.fill")
                            Text(progress?.isSuccess == true ? "Re-Flash / Verify Firmware" : "Install Driver & Flash Firmware")
                                .font(.body.weight(.semibold))
                        }
                        .padding(.horizontal, 16)
                        .padding(.vertical, 8)
                        .background(progress?.isSuccess == true ? Color.green : Color.blue)
                        .foregroundStyle(.white)
                        .clipShape(RoundedRectangle(cornerRadius: 6))
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(18)
            .background(Color(nsColor: .windowBackgroundColor))
        }
        .frame(minWidth: 640, minHeight: 640)
    }

    private func defaultPlaceholderSteps() -> [InstallStep] {
        [
            InstallStep(index: 1, name: "Hardware Preflight", description: "Inspect USB bus, verify power state & VID:PID match", state: "pending"),
            InstallStep(index: 2, name: "Firmware Integrity", description: "Verify SHA-256 checksums & load vendor microcode blobs", state: "pending"),
            InstallStep(index: 3, name: "ZeroCD ModeSwitch", description: "Execute SCSI Eject to switch storage mode to WLAN", state: "pending"),
            InstallStep(index: 4, name: "RAM / Flash Upload", description: "Upload Baseband patch & jump to Operational vector", state: "pending"),
            InstallStep(index: 5, name: "Network Stack Activation", description: "Configure utun10 virtual network tunnel & routing tables", state: "pending"),
            InstallStep(index: 6, name: "End-to-End Validation", description: "Transmit loopback probe & verify gateway ICMP reachability", state: "pending"),
        ]
    }

    private func logColor(_ log: String) -> Color {
        if log.contains("🎉") || log.contains("Successfully") || log.contains("verified") { return .green }
        if log.contains("❌") || log.contains("aborted") || log.contains("failed") { return .red }
        if log.contains("Warning") || log.contains("re-enumerated") { return .yellow }
        return .green.opacity(0.85)
    }
}

// MARK: - Step Row View
struct StepRowView: View {
    let step: InstallStep

    var body: some View {
        HStack(spacing: 12) {
            ZStack {
                Circle()
                    .fill(stepBgColor(step.state))
                    .frame(width: 26, height: 26)

                if step.state == "completed" {
                    Image(systemName: "checkmark")
                        .font(.system(size: 11, weight: .bold))
                        .foregroundStyle(.white)
                } else if step.state == "in_progress" {
                    ProgressView()
                        .scaleEffect(0.55)
                } else if step.state == "failed" {
                    Image(systemName: "xmark")
                        .font(.system(size: 11, weight: .bold))
                        .foregroundStyle(.white)
                } else {
                    Text("\(step.index)")
                        .font(.system(size: 11, weight: .bold))
                        .foregroundStyle(.secondary)
                }
            }

            VStack(alignment: .leading, spacing: 2) {
                Text(step.name)
                    .font(.body.weight(.semibold))
                Text(step.details ?? step.description)
                    .font(.caption2)
                    .foregroundStyle(step.state == "failed" ? .red : .secondary)
            }

            Spacer()

            Text(step.state.uppercased())
                .font(.system(size: 9, weight: .bold))
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(stepTagBgColor(step.state))
                .foregroundStyle(stepTagTextColor(step.state))
                .clipShape(Capsule())
        }
        .padding(10)
        .background(Color(nsColor: .controlBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(step.state == "in_progress" ? Color.blue.opacity(0.4) : Color.secondary.opacity(0.1), lineWidth: 1)
        )
    }

    private func stepBgColor(_ state: String) -> Color {
        switch state {
        case "completed": return .green
        case "in_progress": return .blue.opacity(0.15)
        case "failed": return .red
        default: return Color.secondary.opacity(0.15)
        }
    }

    private func stepTagBgColor(_ state: String) -> Color {
        switch state {
        case "completed": return .green.opacity(0.15)
        case "in_progress": return .blue.opacity(0.15)
        case "failed": return .red.opacity(0.15)
        default: return Color.secondary.opacity(0.1)
        }
    }

    private func stepTagTextColor(_ state: String) -> Color {
        switch state {
        case "completed": return .green
        case "in_progress": return .blue
        case "failed": return .red
        default: return .secondary
        }
    }
}

// MARK: - Architecture Option Card
struct ArchitectureOptionCard: View {
    let title: String
    let subtitle: String
    let icon: String
    let isSelected: Bool
    let badge: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Image(systemName: icon)
                    .font(.title2)
                    .foregroundStyle(isSelected ? .blue : .secondary)
                    .frame(width: 32)

                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        Text(title)
                            .font(.body.weight(.semibold))
                        Text(badge)
                            .font(.system(size: 8, weight: .bold))
                            .padding(.horizontal, 4)
                            .padding(.vertical, 1)
                            .background(isSelected ? Color.blue : Color.secondary.opacity(0.15))
                            .foregroundStyle(isSelected ? Color.white : Color.secondary)
                            .clipShape(Capsule())
                    }
                    Text(subtitle)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.leading)
                }

                Spacer()

                Image(systemName: isSelected ? "checkmark.circle.fill" : "circle")
                    .foregroundStyle(isSelected ? .blue : .secondary.opacity(0.5))
            }
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(nsColor: .controlBackgroundColor))
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .overlay(
                RoundedRectangle(cornerRadius: 8)
                    .stroke(isSelected ? Color.blue : Color.secondary.opacity(0.15), lineWidth: isSelected ? 1.5 : 1)
            )
        }
        .buttonStyle(.plain)
    }
}
