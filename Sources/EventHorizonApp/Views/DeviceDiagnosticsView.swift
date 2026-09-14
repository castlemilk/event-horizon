import SwiftUI
import EventHorizonCore

struct DeviceDiagnosticsView: View {
    let node: HardwareTopologyNode
    let stat: InterfaceStat?
    @State private var diagnostics = DeviceDiagnosticsStore()
    @State private var target = "1.1.1.1"
    @State private var request: Task<Void, Never>?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label(node.interfaceName.isEmpty ? "No network interface" : "Test adapter: \(node.interfaceName)", systemImage: "network")
                .font(.headline)
            if let reason = node.diagnosticsUnavailableReason(stat: stat) {
                Text(reason).font(.callout).foregroundStyle(.secondary)
            }
            HStack {
                TextField("Ping target", text: $target)
                    .textFieldStyle(.roundedBorder)
                    .accessibilityLabel("Ping target IP address or hostname")
                Button("Ping") {
                    request = Task { await diagnostics.runPing(node: node, stat: stat, target: target) }
                }
                .disabled(target.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                Button("Speed Test") {
                    request = Task { await diagnostics.runSpeedTest(node: node, stat: stat) }
                }
            }
            .disabled(diagnostics.isRunning || node.diagnosticsUnavailableReason(stat: stat) != nil)
            Text("Tests use this adapter’s interface. Speed tests transfer data over the internet; a local-only dongle link may not reach the test server.")
                .font(.caption).foregroundStyle(.secondary)
            if diagnostics.isRunning {
                HStack {
                    ProgressView().controlSize(.small)
                    Text("Testing \(node.interfaceName)…").font(.callout)
                    Button("Cancel", action: cancel)
                }
            }
            if let error = diagnostics.error {
                Label(error, systemImage: "exclamationmark.triangle").foregroundStyle(.orange)
            }
            ForEach(diagnostics.pings) { ping in
                VStack(alignment: .leading, spacing: 3) {
                    Label(ping.summary, systemImage: ping.isReachable ? "checkmark.circle" : "xmark.circle")
                        .foregroundStyle(ping.isReachable ? .primary : .secondary)
                    if let method = ping.method {
                        Text("\(method.uppercased()) · \(node.interfaceName) · source \(ping.sourceIP ?? "unavailable")")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
            if let speed = diagnostics.speed {
                HStack(spacing: 24) {
                    MetricSummaryTile(icon: "arrow.down", label: "Download", value: speed.hasMeasuredDownload ? String(format: "%.1f Mbps", speed.downloadMbps) : "Unavailable")
                    MetricSummaryTile(icon: "arrow.up", label: "Upload", value: speed.hasMeasuredUpload ? String(format: "%.1f Mbps", speed.uploadMbps) : "Unavailable")
                    MetricSummaryTile(icon: "timer", label: "Latency", value: speed.latencyMs >= 0 ? "\(speed.latencyMs) ms" : "Unavailable")
                }
                Text("\(speed.interface) · source \(speed.sourceIP ?? "unavailable") · \(speed.status)")
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
        .onDisappear(perform: cancel)
        .onChange(of: node) { old, new in
            if old.interfaceName != new.interfaceName || old.ipAddress != new.ipAddress || old.status != new.status { cancel() }
        }
        .onChange(of: stat?.isUp) { _, up in if up != true { cancel() } }
    }

    private func cancel() {
        request?.cancel()
        request = nil
        diagnostics.reset()
    }
}

struct DeviceTelemetryView: View {
    let node: HardwareTopologyNode
    let stat: InterfaceStat?

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if let stat, stat.name == node.interfaceName {
                Label(stat.isUp ? "\(stat.name) is up" : "\(stat.name) is down", systemImage: stat.isUp ? "network" : "exclamationmark.circle")
                HStack(spacing: 20) {
                    MetricSummaryTile(icon: "arrow.down", label: "Receive", value: rate(stat.rxRateKBps))
                    MetricSummaryTile(icon: "arrow.up", label: "Transmit", value: rate(stat.txRateKBps))
                    MetricSummaryTile(icon: "shippingbox", label: "Packets in / out", value: "\(stat.packetsIn) / \(stat.packetsOut)")
                }
                HStack(spacing: 20) {
                    MetricSummaryTile(icon: "arrow.down.doc", label: "Bytes received", value: ByteCountFormatter.string(fromByteCount: Int64(clamping: stat.bytesIn), countStyle: .binary))
                    MetricSummaryTile(icon: "arrow.up.doc", label: "Bytes sent", value: ByteCountFormatter.string(fromByteCount: Int64(clamping: stat.bytesOut), countStyle: .binary))
                    MetricSummaryTile(icon: "exclamationmark.triangle", label: "Errors in / out", value: "\(stat.errorsIn) / \(stat.errorsOut)")
                }
                Text(node.interfaceName.hasPrefix("utun") ? "Traffic counters are measured at the dongle’s tunnel interface. Radio signal, noise and PHY rate are unavailable from this driver." : "Traffic counters are measured by macOS for this interface. Radio metrics appear only when the driver reports them.")
                    .font(.caption).foregroundStyle(.secondary)
            } else {
                Label("Telemetry unavailable", systemImage: "waveform.path")
                Text(node.diagnosticsUnavailableReason(stat: stat) ?? "Waiting for this adapter’s counters.")
                    .foregroundStyle(.secondary)
            }
            if !node.speed.isEmpty { Text("USB / bus speed: \(node.speed)").font(.caption) }
        }
    }

    private func rate(_ value: Double) -> String {
        value.isFinite && value >= 0 ? String(format: "%.1f KiB/s", value) : "Unavailable"
    }
}
