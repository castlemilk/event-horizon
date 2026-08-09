import SwiftUI
import StarlinkWiFiCore

public struct LiveMetricsAnalyticsView: View {
    let stat: InterfaceStat?
    let pings: [PingResult]

    @State private var selectedInterface = "USB Wi-Fi 6E Adapter"
    @State private var selectedTimeRange = "24 Hours"

    public init(stat: InterfaceStat?, pings: [PingResult]) {
        self.stat = stat
        self.pings = pings
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            // Header Bar Selectors
            HStack {
                HStack(spacing: 6) {
                    Text("Interface")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Picker("", selection: $selectedInterface) {
                        Text("USB Wi-Fi 6E Adapter").tag("USB Wi-Fi 6E Adapter")
                        Text("Realtek 2.5G Ethernet").tag("Realtek 2.5G Ethernet")
                        Text("Built-in Wi-Fi").tag("Built-in Wi-Fi")
                    }
                    .pickerStyle(.menu)
                    .frame(width: 190)
                }

                Spacer()

                HStack(spacing: 6) {
                    Text("Time range")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Picker("", selection: $selectedTimeRange) {
                        Text("24 Hours").tag("24 Hours")
                        Text("12 Hours").tag("12 Hours")
                        Text("1 Hour").tag("1 Hour")
                    }
                    .pickerStyle(.menu)
                    .frame(width: 120)
                }
            }

            // 4-Grid Charts Layout (Matching Frame 5 of mockup)
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 14) {
                // 1. Signal Strength Chart Card
                MetricChartCard(
                    title: "Signal strength",
                    value: "-42 dBm",
                    badgeText: "Excellent",
                    badgeColor: .green,
                    lineColor: .green,
                    chartData: [-55, -50, -48, -44, -46, -42, -40, -43, -42],
                    minVal: -80,
                    maxVal: -30
                )

                // 2. Latency Chart Card
                MetricChartCard(
                    title: "Latency",
                    value: "\(pings.first?.rttMs ?? 12) ms",
                    badgeText: "Excellent",
                    badgeColor: .green,
                    lineColor: .blue,
                    chartData: [22, 18, 15, 14, 16, 12, 14, 18, 12],
                    minVal: 0,
                    maxVal: 40
                )

                // 3. Download Chart Card
                MetricChartCard(
                    title: "Download",
                    value: "\(Int(stat?.rxRateKBps ?? 312)) Mbps",
                    badgeText: "",
                    badgeColor: .blue,
                    lineColor: .blue,
                    chartData: [120, 150, 140, 200, 260, 220, 300, 280, 312],
                    minVal: 0,
                    maxVal: 400
                )

                // 4. Upload Chart Card
                MetricChartCard(
                    title: "Upload",
                    value: "\(Int(stat?.txRateKBps ?? 68)) Mbps",
                    badgeText: "",
                    badgeColor: .teal,
                    lineColor: .teal,
                    chartData: [25, 40, 35, 45, 55, 50, 62, 58, 68],
                    minVal: 0,
                    maxVal: 100
                )
            }

            // Footer note
            HStack {
                Circle()
                    .fill(Color.green)
                    .frame(width: 6, height: 6)
                Text("Live data updates every 5 seconds")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                Spacer()
                Text("Data for the selected 24-hour range")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            .padding(.top, 4)
        }
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
                Text("24h")
                Spacer()
                Text("18h")
                Spacer()
                Text("12h")
                Spacer()
                Text("6h")
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
