import SwiftUI
import EventHorizonCore

public struct RFSpectrumAnalyzerView: View {
    @Bindable var store: WiFiManagerStore
    @State private var selectedBand: String = "2.4GHz"

    public init(store: WiFiManagerStore) {
        self.store = store
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            // Header with Band Selector & Refresh
            HStack {
                HStack(spacing: 8) {
                    Image(systemName: "waveform.path.ecg")
                        .font(.title2)
                        .foregroundStyle(.cyan)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("RF Spectrum & Channel Analyzer")
                            .font(.headline)
                        Text("Co-channel density & 802.11 interference telemetry")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }

                Spacer()

                // Band Picker
                Picker("Band", selection: $selectedBand) {
                    Text("2.4 GHz").tag("2.4GHz")
                    Text("5 GHz").tag("5GHz")
                }
                .pickerStyle(.segmented)
                .frame(width: 160)

                Button {
                    Task {
                        await store.fetchSpectrumReport()
                    }
                } label: {
                    Image(systemName: "arrow.clockwise")
                        .font(.subheadline)
                }
                .buttonStyle(.plain)
                .padding(6)
                .background(Color.secondary.opacity(0.12))
                .clipShape(Circle())
            }

            Divider()

            // Recommendations Card
            if let report = store.spectrumReport {
                HStack(spacing: 16) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Optimal 2.4 GHz Channel")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                        HStack(spacing: 4) {
                            Text("Channel \(report.optimalChannel24GHz)")
                                .font(.callout.weight(.bold))
                                .foregroundStyle(.green)
                            Text("(Non-Overlapping)")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .padding(10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color.green.opacity(0.08))
                    .clipShape(RoundedRectangle(cornerRadius: 8))

                    VStack(alignment: .leading, spacing: 4) {
                        Text("Optimal 5 GHz Channel")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                        HStack(spacing: 4) {
                            Text("Channel \(report.optimalChannel5GHz)")
                                .font(.callout.weight(.bold))
                                .foregroundStyle(.cyan)
                            Text("(80 MHz Width)")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .padding(10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color.cyan.opacity(0.08))
                    .clipShape(RoundedRectangle(cornerRadius: 8))
                }
            }

            // Spectrum Graph
            VStack(alignment: .leading, spacing: 8) {
                Text(selectedBand == "2.4GHz" ? "2.4 GHz Spectrum (Channels 1 - 13)" : "5 GHz UNII Spectrum (Channels 36 - 165)")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)

                let channels = selectedBand == "2.4GHz" ? (store.spectrumReport?.channels24GHz ?? []) : (store.spectrumReport?.channels5GHz ?? [])

                if channels.isEmpty {
                    VStack(spacing: 8) {
                        ProgressView()
                            .scaleEffect(0.8)
                        Text("Scanning RF spectrum...")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity, minHeight: 120)
                } else {
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(alignment: .bottom, spacing: selectedBand == "2.4GHz" ? 12 : 8) {
                            ForEach(channels) { ch in
                                ChannelBarView(info: ch)
                            }
                        }
                        .padding(.vertical, 10)
                        .padding(.horizontal, 4)
                    }
                }
            }
            .padding(12)
            .background(Color(nsColor: .controlBackgroundColor).opacity(0.5))
            .clipShape(RoundedRectangle(cornerRadius: 10))
        }
        .padding(14)
        .background(Color(nsColor: .windowBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 12))
        .shadow(color: Color.black.opacity(0.04), radius: 3, x: 0, y: 1)
        .task {
            if store.spectrumReport == nil {
                await store.fetchSpectrumReport()
            }
        }
    }
}

struct ChannelBarView: View {
    let info: RFChannelInfo

    var congestionColor: Color {
        switch info.congestionLevel {
        case "Clean": return .green
        case "Low": return .cyan
        case "Moderate": return .orange
        default: return .red
        }
    }

    var barHeight: CGFloat {
        let maxCount = 6.0
        let h = (CGFloat(info.bssidCount) / maxCount) * 80.0
        return max(h, 8)
    }

    var body: some View {
        VStack(spacing: 4) {
            // Count badge
            if info.bssidCount > 0 {
                Text("\(info.bssidCount)")
                    .font(.system(size: 9, weight: .bold))
                    .foregroundStyle(congestionColor)
            } else {
                Text("—")
                    .font(.system(size: 9))
                    .foregroundStyle(.secondary.opacity(0.5))
            }

            // Spectrum Bar
            ZStack(alignment: .bottom) {
                Capsule()
                    .fill(Color.secondary.opacity(0.12))
                    .frame(width: 24, height: 90)

                Capsule()
                    .fill(
                        LinearGradient(
                            colors: [congestionColor.opacity(0.8), congestionColor],
                            startPoint: .bottom,
                            endPoint: .top
                        )
                    )
                    .frame(width: 24, height: barHeight)
            }

            // Channel Label
            Text("Ch \(info.channel)")
                .font(.system(size: 10, weight: info.isNonOverlapping ? .bold : .regular))
                .foregroundStyle(info.isNonOverlapping ? .primary : .secondary)

            // Frequency
            Text("\(info.frequencyMhz)")
                .font(.system(size: 8))
                .foregroundStyle(.secondary)
        }
    }
}
