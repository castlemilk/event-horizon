import SwiftUI
import EventHorizonCore

public struct SpeedtestView: View {
    @Bindable var store: WiFiManagerStore

    public init(store: WiFiManagerStore) {
        self.store = store
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            // Header
            HStack {
                HStack(spacing: 8) {
                    Image(systemName: "gauge.with.needle")
                        .font(.title2)
                        .foregroundStyle(.blue)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Multi-Stream Speedtest")
                            .font(.headline)
                        Text("Measured download, upload and latency for the selected adapter")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }

                Spacer()

                Button {
                    Task {
                        await store.startMultiStreamSpeedTest(interface: store.selectedInterface)
                    }
                } label: {
                    HStack(spacing: 6) {
                        if store.isRunningSpeedTest {
                            ProgressView()
                                .scaleEffect(0.6)
                        } else {
                            Image(systemName: "play.fill")
                        }
                        Text(store.isRunningSpeedTest ? "Testing..." : "Start Test")
                    }
                    .font(.caption.weight(.bold))
                }
                .buttonStyle(.borderedProminent)
                .disabled(store.isRunningSpeedTest || !store.canRunSelectedInterfaceDiagnostics)
            }

            Divider()

            Picker("Test adapter", selection: Binding(get: { store.selectedInterface }, set: { store.selectDeviceInterface($0) })) {
                Text(store.selectedInterface.isEmpty ? "Select an adapter" : store.selectedInterface).tag(store.selectedInterface)
                ForEach(store.topologyNodes.filter { !$0.interfaceName.isEmpty && $0.interfaceName != store.selectedInterface }) { node in
                    Text("\(node.usbDriver) · \(node.interfaceName)").tag(node.interfaceName)
                }
            }
            .disabled(store.isRunningSpeedTest)
            if !store.canRunSelectedInterfaceDiagnostics {
                Text("Select a connected adapter with an IP address to run a test.").font(.caption).foregroundStyle(.secondary)
            }
            if let error = store.speedTestError ?? store.speedTestReport?.error {
                Label(error, systemImage: "exclamationmark.triangle").font(.callout).foregroundStyle(.orange)
            }
            Text("Uses internet data. Results belong to the named interface; local-only links may not reach the test server.")
                .font(.caption).foregroundStyle(.secondary)

            // Metrics Row (Download, Upload, Ping, Jitter)
            let report = store.speedTestReport?.interface == store.selectedInterface ? store.speedTestReport : nil
            HStack(spacing: 12) {
                SpeedMetricCard(
                    title: "DOWNLOAD",
                    value: report.map { $0.bytesReceived > 0 ? String(format: "%.1f", $0.downloadMbps) : "—" } ?? "—",
                    unit: "Mbps",
                    icon: "arrow.down.circle.fill",
                    color: .blue,
                    isActive: report?.phase == "download"
                )

                SpeedMetricCard(
                    title: "UPLOAD",
                    value: report.map { $0.bytesSent > 0 ? String(format: "%.1f", $0.uploadMbps) : "—" } ?? "—",
                    unit: "Mbps",
                    icon: "arrow.up.circle.fill",
                    color: .purple,
                    isActive: report?.phase == "upload"
                )

                SpeedMetricCard(
                    title: "PING",
                    value: report.map { $0.pingMs >= 0 ? "\($0.pingMs)" : "—" } ?? "—",
                    unit: "ms",
                    icon: "timer",
                    color: .green,
                    isActive: report?.phase == "ping"
                )

                SpeedMetricCard(
                    title: "JITTER",
                    value: report.map { $0.jitterMs >= 0 ? String(format: "%.1f", $0.jitterMs) : "—" } ?? "—",
                    unit: "ms",
                    icon: "waveform.path",
                    color: .orange,
                    isActive: false
                )
            }

            if let report {
                Text("\(report.interface) · source \(report.sourceIP ?? "unavailable") · \(report.phase)")
                    .font(.caption.monospaced()).foregroundStyle(.secondary)
            }

            // Progress Bar if running
            if store.isRunningSpeedTest, let progress = report?.progressPercent {
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        Text("Phase: \(report?.phase.uppercased() ?? "RUNNING")")
                            .font(.system(size: 10, weight: .bold))
                            .foregroundStyle(.secondary)
                        Spacer()
                        Text("\(Int(progress))%")
                            .font(.system(size: 10, weight: .bold))
                            .foregroundStyle(.blue)
                    }
                    ProgressView(value: progress, total: 100.0)
                        .tint(.blue)
                }
                .padding(.top, 4)
            }
        }
        .padding(14)
        .background(Color(nsColor: .windowBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 12))
        .shadow(color: Color.black.opacity(0.04), radius: 3, x: 0, y: 1)
    }
}

struct SpeedMetricCard: View {
    let title: String
    let value: String
    let unit: String
    let icon: String
    let color: Color
    let isActive: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 4) {
                Image(systemName: icon)
                    .font(.caption2)
                    .foregroundStyle(color)
                Text(title)
                    .font(.system(size: 9, weight: .bold))
                    .foregroundStyle(.secondary)
            }

            HStack(alignment: .firstTextBaseline, spacing: 2) {
                Text(value)
                    .font(.system(size: 20, weight: .heavy, design: .rounded))
                    .foregroundStyle(isActive ? color : .primary)
                Text(unit)
                    .font(.system(size: 10, weight: .semibold))
                    .foregroundStyle(.secondary)
            }
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(isActive ? color.opacity(0.12) : Color(nsColor: .controlBackgroundColor).opacity(0.5))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(isActive ? color : Color.clear, lineWidth: 1.5)
        )
    }
}
