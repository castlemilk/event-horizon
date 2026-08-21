import SwiftUI
import EventHorizonCore

public struct LiveMetricsAnalyticsView: View {
    let stat: InterfaceStat?
    let pings: [PingResult]
    let hotspot: AccessPoint?
    let interfaces: [String]
    let selectedInterface: String
    let signalHistory: [Double]
    let latencyHistory: [Double]
    let rxHistory: [Double]
    let txHistory: [Double]
    let onSelectInterface: (String) -> Void

    public init(
        stat: InterfaceStat?,
        pings: [PingResult],
        hotspot: AccessPoint? = nil,
        interfaces: [String] = [],
        selectedInterface: String = "",
        signalHistory: [Double] = [],
        latencyHistory: [Double] = [],
        rxHistory: [Double] = [],
        txHistory: [Double] = [],
        onSelectInterface: @escaping (String) -> Void = { _ in }
    ) {
        self.stat = stat
        self.pings = pings
        self.hotspot = hotspot
        self.interfaces = interfaces
        self.selectedInterface = selectedInterface
        self.signalHistory = signalHistory
        self.latencyHistory = latencyHistory
        self.rxHistory = rxHistory
        self.txHistory = txHistory
        self.onSelectInterface = onSelectInterface
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            // Interface Selector
            HStack {
                HStack(spacing: 6) {
                    Text("Interface")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    if interfaces.isEmpty {
                        Text("No interfaces detected")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    } else {
                        Picker("", selection: Binding(
                            get: { selectedInterface },
                            set: { onSelectInterface($0) }
                        )) {
                            ForEach(interfaces, id: \.self) { name in
                                Text(name).tag(name)
                            }
                        }
                        .pickerStyle(.menu)
                        .frame(width: 140)
                    }
                }

                Spacer()

                HStack(spacing: 6) {
                    Circle()
                        .fill(Color.green)
                        .frame(width: 6, height: 6)
                    Text("Live samples every 3 seconds")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            // 4-Grid Charts Layout
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 14) {
                // 1. Signal Strength Chart Card
                MetricChartCard(
                    title: "Signal strength",
                    value: signalValue,
                    badgeText: signalBadge.0,
                    badgeColor: signalBadge.1,
                    lineColor: .green,
                    chartData: signalHistory,
                    minVal: -80,
                    maxVal: -30
                )

                // 2. Latency Chart Card
                MetricChartCard(
                    title: "Latency",
                    value: latencyValue,
                    badgeText: latencyBadge.0,
                    badgeColor: latencyBadge.1,
                    lineColor: .blue,
                    chartData: latencyHistory,
                    minVal: 0,
                    maxVal: latencyScaleMax
                )

                // 3. Download Chart Card
                MetricChartCard(
                    title: "Download",
                    value: downloadValue,
                    badgeText: "",
                    badgeColor: .blue,
                    lineColor: .blue,
                    chartData: rxHistory,
                    minVal: 0,
                    maxVal: throughputScaleMax
                )

                // 4. Upload Chart Card
                MetricChartCard(
                    title: "Upload",
                    value: uploadValue,
                    badgeText: "",
                    badgeColor: .teal,
                    lineColor: .teal,
                    chartData: txHistory,
                    minVal: 0,
                    maxVal: throughputScaleMax
                )
            }

            // Footer note
            HStack {
                Text("No data yet — waiting for first live sample.")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .opacity(allHistoriesEmpty ? 1 : 0)
                Spacer()
            }
            .padding(.top, 4)
        }
    }

    private var signalValue: String {
        guard let hotspot, hotspot.rssi != 0 else { return "—" }
        return "\(hotspot.rssi) dBm"
    }

    private var signalBadge: (String, Color) {
        guard let hotspot, hotspot.rssi != 0 else { return ("", .green) }
        switch hotspot.rssi {
        case ..<(-70): return ("Weak", .orange)
        case ..<(-60): return ("Fair", .yellow)
        default: return ("Excellent", .green)
        }
    }

    private var latencyValue: String {
        guard let rtt = pings.first?.rttMs, rtt > 0 else { return "—" }
        return "\(rtt) ms"
    }

    private var latencyBadge: (String, Color) {
        guard let rtt = pings.first?.rttMs, rtt > 0 else { return ("", .green) }
        switch rtt {
        case ..<30: return ("Excellent", .green)
        case ..<60: return ("Good", .yellow)
        default: return ("Degraded", .orange)
        }
    }

    private var downloadValue: String {
        guard let rate = stat?.rxRateKBps, rate > 0 else { return "—" }
        return "\(Int(rate)) KB/s"
    }

    private var uploadValue: String {
        guard let rate = stat?.txRateKBps, rate > 0 else { return "—" }
        return "\(Int(rate)) KB/s"
    }

    private var latencyScaleMax: Double {
        let peak = latencyHistory.max() ?? 0
        return max(peak * 1.2, 10)
    }

    private var throughputScaleMax: Double {
        let peak = (rxHistory + txHistory).max() ?? 0
        return max(peak * 1.1, 1)
    }

    private var allHistoriesEmpty: Bool {
        signalHistory.isEmpty && latencyHistory.isEmpty && rxHistory.isEmpty && txHistory.isEmpty
    }
}

struct MetricChartCard: View {
    let title: String
    let value: String
    let badgeText: String
    let badgeColor: Color
    let lineColor: Color
    let chartData: [Double]
    var minVal: Double = 0.0
    var maxVal: Double = 100.0

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(title)
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)

                Spacer()

                if !badgeText.isEmpty {
                    Text(badgeText)
                        .font(.caption2.weight(.medium))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(badgeColor.opacity(0.15))
                        .foregroundStyle(badgeColor)
                        .clipShape(Capsule())
                }
            }

            Text(value)
                .font(.title2.weight(.bold).monospacedDigit())

            ZStack(alignment: .bottomLeading) {
                // Background grid lines
                VStack(spacing: 0) {
                    Divider()
                    Spacer()
                    Divider()
                    Spacer()
                    Divider()
                }
                .foregroundStyle(Color.secondary.opacity(0.1))

                LineGraphAreaShape(data: chartData, minVal: minVal, maxVal: maxVal)
                    .fill(
                        LinearGradient(
                            colors: [lineColor.opacity(0.18), lineColor.opacity(0.0)],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                    )

                LineGraphShape(data: chartData, minVal: minVal, maxVal: maxVal)
                    .stroke(lineColor, style: StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
            }
            .frame(height: 70)

            HStack {
                Text("older")
                Spacer()
                Text("Now")
            }
            .font(.system(size: 8))
            .foregroundStyle(.secondary)
        }
        .padding(14)
        .background(Color(nsColor: .windowBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .overlay(
            RoundedRectangle(cornerRadius: 10)
                .stroke(Color.secondary.opacity(0.12), lineWidth: 1)
        )
    }
}
