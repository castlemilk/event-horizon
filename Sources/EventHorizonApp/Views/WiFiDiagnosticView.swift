import SwiftUI
import EventHorizonCore

public struct WiFiDiagnosticView: View {
    @Bindable var store: WiFiManagerStore
    @State private var selectedFilter: DiagnosticFilter = .all
    @State private var selectedTargetInterface: String = "en0"

    public enum DiagnosticFilter: String, CaseIterable, Identifiable, Sendable {
        case all = "All Results"
        case icmp = "ICMP Ping"
        case http = "HTTP / TLS"
        case dns = "DNS Benchmark"
        case otel = "OTel Exporter"

        public var id: String { rawValue }
        public var icon: String {
            switch self {
            case .all: return "square.grid.2x2"
            case .icmp: return "waveform.path"
            case .http: return "network"
            case .dns: return "globe"
            case .otel: return "chart.bar.fill"
            }
        }
    }

    public init(store: WiFiManagerStore) {
        self.store = store
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            // Execution Control & Interface Selector Bar
            HStack(spacing: 12) {
                HStack(spacing: 6) {
                    Image(systemName: "antenna.radiowaves.left.and.right")
                        .foregroundStyle(.blue)
                    Text("Target Adapter:")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.secondary)
                    
                    Picker("Interface", selection: $selectedTargetInterface) {
                        ForEach(availableInterfaces, id: \.self) { iface in
                            Text(ifaceLabel(for: iface)).tag(iface)
                        }
                    }
                    .pickerStyle(.menu)
                    .frame(width: 220)
                }

                Spacer()

                if let localIp = currentReport?.localIp, !localIp.isEmpty {
                    HStack(spacing: 4) {
                        Text("IP:")
                            .font(.caption2.weight(.bold))
                            .foregroundStyle(.secondary)
                        Text(localIp)
                            .font(.caption.monospaced())
                            .foregroundStyle(.primary)
                    }
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(Color(nsColor: .controlBackgroundColor))
                    .clipShape(RoundedRectangle(cornerRadius: 6))
                }

                Button(action: {
                    Task {
                        await store.runFullDiagnostics(interface: selectedTargetInterface)
                    }
                }) {
                    HStack(spacing: 6) {
                        if store.isRunningDiagnostics {
                            ProgressView()
                                .controlSize(.small)
                        } else {
                            Image(systemName: "play.fill")
                                .font(.caption2)
                        }
                        Text(store.isRunningDiagnostics ? "Running Tests..." : "Run Full Diagnostic Suite")
                            .font(.callout.weight(.medium))
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(store.isRunningDiagnostics)
            }
            .padding(12)
            .background(Color(nsColor: .controlBackgroundColor))
            .clipShape(RoundedRectangle(cornerRadius: 10))
            .overlay(
                RoundedRectangle(cornerRadius: 10)
                    .stroke(Color.secondary.opacity(0.12), lineWidth: 1)
            )

            // Diagnostic Quality Score & Link Analytics Hero Card
            if let report = currentReport {
                DiagnosticScoreHeroCard(report: report, stability: store.stabilityStats)
            }

            // Protocol Filter Tabs
            Picker("Filter", selection: $selectedFilter) {
                ForEach(DiagnosticFilter.allCases) { filter in
                    Label(filter.rawValue, systemImage: filter.icon).tag(filter)
                }
            }
            .pickerStyle(.segmented)

            // Test Sections Content
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    if selectedFilter == .all || selectedFilter == .icmp {
                        ICMPPingSection(pings: currentReport?.pings ?? store.pingResults)
                    }

                    if selectedFilter == .all || selectedFilter == .http {
                        if let probes = currentReport?.httpProbes, !probes.isEmpty {
                            HTTPProbeSection(probes: probes)
                        }
                    }

                    if selectedFilter == .all || selectedFilter == .dns {
                        if let dnsProbes = currentReport?.dnsProbes, !dnsProbes.isEmpty {
                            DNSBenchmarkSection(probes: dnsProbes)
                        }
                    }

                    if selectedFilter == .all || selectedFilter == .otel {
                        OTelExporterSection(stability: store.stabilityStats)
                    }
                }
                .padding(.bottom, 20)
            }
        }
        .onAppear {
            if !store.selectedInterface.isEmpty {
                selectedTargetInterface = store.selectedInterface
            }
            if currentReport == nil {
                Task {
                    await store.runFullDiagnostics(interface: selectedTargetInterface)
                }
            }
        }
    }

    private var currentReport: DiagnosticSuiteReport? {
        store.diagnosticReport
    }

    private var availableInterfaces: [String] {
        let list = store.topologyNodes.map(\.bsdInterface).filter { !$0.isEmpty }
        if list.isEmpty { return ["en0", "utun10"] }
        return list
    }

    private func ifaceLabel(for iface: String) -> String {
        if let node = store.topologyNodes.first(where: { $0.bsdInterface == iface }) {
            return "\(iface) — \(node.usbDriver)"
        }
        return iface
    }
}

// MARK: - Quality Score Hero Card
struct DiagnosticScoreHeroCard: View {
    let report: DiagnosticSuiteReport
    let stability: StabilityStats?

    var body: some View {
        HStack(spacing: 20) {
            // Grade Badge & Overall Score
            VStack(spacing: 4) {
                ZStack {
                    Circle()
                        .stroke(gradeColor.opacity(0.2), lineWidth: 6)
                        .frame(width: 72, height: 72)
                    
                    Circle()
                        .trim(from: 0, to: CGFloat(min(1.0, report.qualityScore / 100.0)))
                        .stroke(gradeColor, style: StrokeStyle(lineWidth: 6, lineCap: .round))
                        .rotationEffect(.degrees(-90))
                        .frame(width: 72, height: 72)

                    Text(report.qualityGrade)
                        .font(.title.weight(.heavy))
                        .foregroundStyle(gradeColor)
                }

                Text("\(String(format: "%.1f", report.qualityScore))% Score")
                    .font(.caption.weight(.bold).monospacedDigit())
                    .foregroundStyle(.secondary)
            }
            .frame(width: 100)

            Divider()
                .frame(height: 70)

            // Key Analytics Grid
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible()), GridItem(.flexible()), GridItem(.flexible())], spacing: 12) {
                ScoreMetricTile(
                    title: "Avg Latency",
                    value: "\(String(format: "%.1f", report.avgLatencyMs)) ms",
                    subtitle: "Min: \(report.minLatencyMs)ms • Max: \(report.maxLatencyMs)ms",
                    icon: "waveform.path",
                    color: latencyColor(report.avgLatencyMs)
                )

                ScoreMetricTile(
                    title: "Latency Jitter",
                    value: "\(String(format: "%.1f", report.jitterMs)) ms",
                    subtitle: jitterVerdict(report.jitterMs),
                    icon: "waveform.path.ecg",
                    color: jitterColor(report.jitterMs)
                )

                ScoreMetricTile(
                    title: "Packet Loss",
                    value: "\(String(format: "%.1f", report.packetLossPercent))%",
                    subtitle: report.packetLossPercent == 0 ? "Zero loss (Optimal)" : "Degraded Link",
                    icon: "shield.checkerboard",
                    color: report.packetLossPercent == 0 ? .green : .red
                )

                ScoreMetricTile(
                    title: "Link Uptime",
                    value: stability?.uptimeFormatted ?? "100%",
                    subtitle: stability?.currentStatus ?? "Active",
                    icon: "clock.arrow.2.circlepath",
                    color: .purple
                )
            }
        }
        .padding(16)
        .background(
            LinearGradient(
                colors: [Color(nsColor: .controlBackgroundColor), gradeColor.opacity(0.06)],
                startPoint: .leading,
                endPoint: .trailing
            )
        )
        .clipShape(RoundedRectangle(cornerRadius: 12))
        .overlay(
            RoundedRectangle(cornerRadius: 12)
                .stroke(gradeColor.opacity(0.25), lineWidth: 1)
        )
    }

    private var gradeColor: Color {
        switch report.qualityGrade {
        case "A+": return .teal
        case "A": return .green
        case "B": return .blue
        case "C": return .orange
        default: return .red
        }
    }

    private func latencyColor(_ ms: Double) -> Color {
        if ms < 25 { return .green }
        if ms < 75 { return .blue }
        if ms < 150 { return .orange }
        return .red
    }

    private func jitterColor(_ ms: Double) -> Color {
        if ms < 3.0 { return .green }
        if ms < 10.0 { return .orange }
        return .red
    }

    private func jitterVerdict(_ ms: Double) -> String {
        if ms < 2.0 { return "Ultra-stable (eSports ready)" }
        if ms < 8.0 { return "Good stability" }
        return "High variance"
    }
}

struct ScoreMetricTile: View {
    let title: String
    let value: String
    let subtitle: String
    let icon: String
    let color: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 4) {
                Image(systemName: icon)
                    .font(.caption2)
                    .foregroundStyle(color)
                Text(title)
                    .font(.caption2.weight(.semibold))
                    .foregroundStyle(.secondary)
            }
            Text(value)
                .font(.title3.weight(.bold).monospacedDigit())
                .foregroundStyle(.primary)
            Text(subtitle)
                .font(.system(size: 10))
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
    }
}

// MARK: - ICMP Ping Matrix Section
struct ICMPPingSection: View {
    let pings: [PingResult]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Image(systemName: "waveform.path")
                    .foregroundStyle(.blue)
                Text("ICMP REACHABILITY & ROUND-TRIP LATENCY")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(.secondary)
                Spacer()
                Text("\(pings.count) Targets Probed")
                    .font(.caption2.monospaced())
                    .foregroundStyle(.secondary)
            }

            VStack(spacing: 6) {
                ForEach(pings, id: \.target) { ping in
                    PingResultRow(ping: ping)
                }
            }
        }
    }
}

struct PingResultRow: View {
    let ping: PingResult

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: ping.isReachable ? "checkmark.circle.fill" : "xmark.circle.fill")
                .font(.title3)
                .foregroundStyle(ping.isReachable ? .green : .red)

            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(targetTitle(ping.target))
                        .font(.body.weight(.semibold))
                    Text("(\(ping.target))")
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                }
                Text(ping.isReachable ? "Echo reply received via native ICMP socket" : "No route to host / Request timeout")
                    .font(.caption2)
                    .foregroundStyle(ping.isReachable ? Color.secondary : Color.red.opacity(0.8))
            }

            Spacer()

            if ping.isReachable {
                VStack(alignment: .trailing, spacing: 2) {
                    Text("\(ping.rttMs) ms RTT")
                        .font(.callout.monospacedDigit().weight(.bold))
                        .foregroundStyle(pingColor(ping.rttMs))

                    HStack(spacing: 4) {
                        Text("\(String(format: "%.0f%%", ping.packetLossPercent)) Loss")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                        Text("•")
                            .foregroundStyle(.tertiary)
                        Text(speedCategory(ping.rttMs))
                            .font(.system(size: 9, weight: .bold))
                            .padding(.horizontal, 4)
                            .padding(.vertical, 1)
                            .background(pingColor(ping.rttMs).opacity(0.15))
                            .foregroundStyle(pingColor(ping.rttMs))
                            .clipShape(RoundedRectangle(cornerRadius: 3))
                    }
                }
            } else {
                Text("100% PACKET LOSS")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(.red)
            }
        }
        .padding(10)
        .background(Color(nsColor: .controlBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(Color.secondary.opacity(0.1), lineWidth: 1)
        )
    }

    private func targetTitle(_ target: String) -> String {
        switch target {
        case "1.1.1.1": return "Cloudflare Primary DNS"
        case "8.8.8.8": return "Google Public DNS"
        case "9.9.9.9": return "Quad9 Secure DNS"
        case "192.168.100.1": return "Starlink Dish Terminal"
        case "192.168.4.1", "192.168.0.1": return "Default Gateway Router"
        default: return target
        }
    }

    private func pingColor(_ ms: Int64) -> Color {
        if ms < 25 { return .green }
        if ms < 60 { return .blue }
        if ms < 120 { return .orange }
        return .red
    }

    private func speedCategory(_ ms: Int64) -> String {
        if ms < 20 { return "ULTRA-FAST" }
        if ms < 50 { return "FAST" }
        if ms < 100 { return "NORMAL" }
        return "HIGH LATENCY"
    }
}

// MARK: - HTTP Probe Benchmark Section
struct HTTPProbeSection: View {
    let probes: [HTTPProbeResult]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Image(systemName: "network")
                    .foregroundStyle(.purple)
                Text("HTTP & TLS LAYER PROBE TIMINGS (DNS + TCP + TLS + TTFB)")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(.secondary)
                Spacer()
            }

            VStack(spacing: 8) {
                ForEach(probes, id: \.target) { probe in
                    VStack(alignment: .leading, spacing: 8) {
                        HStack {
                            VStack(alignment: .leading, spacing: 1) {
                                HStack(spacing: 6) {
                                    Text(probe.target)
                                        .font(.body.weight(.semibold))
                                    Text(probe.protocolName)
                                        .font(.system(size: 9, weight: .bold).monospaced())
                                        .padding(.horizontal, 4)
                                        .padding(.vertical, 1)
                                        .background(Color.purple.opacity(0.12))
                                        .foregroundStyle(.purple)
                                        .clipShape(RoundedRectangle(cornerRadius: 3))
                                }
                                Text(probe.url)
                                    .font(.caption2.monospaced())
                                    .foregroundStyle(.secondary)
                            }

                            Spacer()

                            VStack(alignment: .trailing, spacing: 2) {
                                HStack(spacing: 6) {
                                    Text("\(probe.statusCode)")
                                        .font(.caption.weight(.bold).monospaced())
                                        .foregroundStyle(probe.isSuccess ? .green : .red)
                                    Text("\(probe.totalMs) ms Total")
                                        .font(.callout.weight(.bold).monospacedDigit())
                                        .foregroundStyle(.primary)
                                }
                            }
                        }

                        // Detailed Timing Breakdown Micro-Cards
                        HStack(spacing: 8) {
                            TimingTag(label: "DNS Lookup", ms: probe.dnsLookupMs, color: .blue)
                            TimingTag(label: "TCP Handshake", ms: probe.tcpHandshakeMs, color: .teal)
                            TimingTag(label: "TLS Handshake", ms: probe.tlsHandshakeMs, color: .purple)
                            TimingTag(label: "TTFB (Response)", ms: probe.ttfbMs, color: .orange)
                        }
                    }
                    .padding(12)
                    .background(Color(nsColor: .controlBackgroundColor))
                    .clipShape(RoundedRectangle(cornerRadius: 8))
                    .overlay(
                        RoundedRectangle(cornerRadius: 8)
                            .stroke(Color.secondary.opacity(0.1), lineWidth: 1)
                    )
                }
            }
        }
    }
}

struct TimingTag: View {
    let label: String
    let ms: Int64
    let color: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .font(.system(size: 9, weight: .medium))
                .foregroundStyle(.secondary)
            HStack(spacing: 2) {
                Circle()
                    .fill(color)
                    .frame(width: 5, height: 5)
                Text("\(ms) ms")
                    .font(.caption2.weight(.bold).monospacedDigit())
            }
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(color.opacity(0.08))
        .clipShape(RoundedRectangle(cornerRadius: 6))
    }
}

// MARK: - DNS Benchmark Section
struct DNSBenchmarkSection: View {
    let probes: [DNSProbeResult]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Image(systemName: "globe")
                    .foregroundStyle(.teal)
                Text("DNS RESOLUTION LATENCY & HOST LOOKUP BENCHMARK")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(.secondary)
                Spacer()
            }

            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 8) {
                ForEach(probes, id: \.domain) { dns in
                    VStack(alignment: .leading, spacing: 6) {
                        HStack {
                            Text(dns.domain)
                                .font(.body.weight(.semibold).monospaced())
                            Spacer()
                            Text("\(dns.resolveTimeMs) ms")
                                .font(.callout.weight(.bold).monospacedDigit())
                                .foregroundStyle(dns.resolveTimeMs < 30 ? .green : .orange)
                        }

                        HStack(spacing: 4) {
                            Text("\(dns.ips.count) A/AAAA Records:")
                                .font(.system(size: 10))
                                .foregroundStyle(.secondary)
                            ForEach(dns.ips.prefix(2), id: \.self) { ip in
                                Text(ip)
                                    .font(.system(size: 9).monospaced())
                                    .padding(.horizontal, 4)
                                    .padding(.vertical, 1)
                                    .background(Color.secondary.opacity(0.1))
                                    .clipShape(RoundedRectangle(cornerRadius: 3))
                            }
                        }
                    }
                    .padding(10)
                    .background(Color(nsColor: .controlBackgroundColor))
                    .clipShape(RoundedRectangle(cornerRadius: 8))
                    .overlay(
                        RoundedRectangle(cornerRadius: 8)
                            .stroke(Color.secondary.opacity(0.1), lineWidth: 1)
                    )
                }
            }
        }
    }
}

// MARK: - OpenTelemetry Exporter Card
struct OTelExporterSection: View {
    let stability: StabilityStats?

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Image(systemName: "chart.bar.fill")
                    .foregroundStyle(.purple)
                Text("OPENTELEMETRY (OTEL) & PROMETHEUS SCRAPE EXPORTER")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(.secondary)
                Spacer()
            }

            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Prometheus Metrics Endpoint Active")
                        .font(.body.weight(.semibold))
                    Text("Scrape URL: http://127.0.0.1:8990/metrics")
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                    Text("Exports interface bandwidth, errors, RX/TX byte counters & uptime gauges.")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }

                Spacer()

                Button(action: {
                    if let url = URL(string: "http://127.0.0.1:8990/metrics") {
                        NSWorkspace.shared.open(url)
                    }
                }) {
                    Label("View Live /metrics", systemImage: "arrow.up.right.square")
                }
                .buttonStyle(.borderedProminent)
                .tint(.purple)
            }
            .padding(14)
            .background(Color.purple.opacity(0.08))
            .clipShape(RoundedRectangle(cornerRadius: 10))
            .overlay(
                RoundedRectangle(cornerRadius: 10)
                    .stroke(Color.purple.opacity(0.2), lineWidth: 1)
            )
        }
    }
}
