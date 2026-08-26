import SwiftUI
import EventHorizonCore

public struct StarlinkDishView: View {
    @Bindable var store: WiFiManagerStore
    @State private var isStowing = false
    @State private var isRebooting = false
    @State private var showConfirmDialog = false
    @State private var dialogAction: DishAction = .stow

    public enum DishAction: String {
        case stow = "Stow Dish"
        case unstow = "Unstow Dish"
        case reboot = "Reboot Dish"
    }

    public init(store: WiFiManagerStore) {
        self.store = store
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            // Header
            HStack {
                HStack(spacing: 10) {
                    ZStack {
                        Circle()
                            .fill(LinearGradient(colors: [.indigo, .purple, .black], startPoint: .topLeading, endPoint: .bottomTrailing))
                            .frame(width: 40, height: 40)
                        Image(systemName: "globe.americas.fill")
                            .font(.title3)
                            .foregroundStyle(.cyan)
                    }
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 8) {
                            Text("Starlink Celestial Telemetry & Radar")
                                .font(.title2.weight(.bold))
                            Text("ONLINE")
                                .font(.system(size: 9, weight: .bold))
                                .padding(.horizontal, 6)
                                .padding(.vertical, 2)
                                .background(Color.green.opacity(0.15))
                                .foregroundStyle(.green)
                                .clipShape(Capsule())
                        }
                        Text("192.168.100.1:9200 • User Terminal (rev3_proto2) • utun10 Uplink")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }

                Spacer()

                // Dish Hardware Controls
                HStack(spacing: 8) {
                    Button {
                        dialogAction = .stow
                        showConfirmDialog = true
                    } label: {
                        Label("Stow", systemImage: "arrow.down.to.line.compact")
                            .font(.caption.weight(.semibold))
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)

                    Button {
                        dialogAction = .reboot
                        showConfirmDialog = true
                    } label: {
                        Label("Reboot", systemImage: "arrow.clockwise")
                            .font(.caption.weight(.semibold))
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }
            }

            Divider()

            // Telemetry Gauges Grid
            HStack(spacing: 12) {
                DishMetricCard(
                    title: "SIGNAL-TO-NOISE (SNR)",
                    value: "9.8",
                    unit: "dB",
                    icon: "antenna.radiowaves.left.and.right",
                    color: .green,
                    badge: "EXCELLENT"
                )

                DishMetricCard(
                    title: "DOWNLINK SPEED",
                    value: "185.0",
                    unit: "Mbps",
                    icon: "arrow.down.circle.fill",
                    color: .blue,
                    badge: "HIGH BANDWIDTH"
                )

                DishMetricCard(
                    title: "UPLINK SPEED",
                    value: "22.4",
                    unit: "Mbps",
                    icon: "arrow.up.circle.fill",
                    color: .purple,
                    badge: "STABLE"
                )

                DishMetricCard(
                    title: "OBSTRUCTION FRACTION",
                    value: "0.0",
                    unit: "%",
                    icon: "shield.slash.fill",
                    color: .cyan,
                    badge: "CLEAR SKY"
                )
            }

            // Radar & Celestial Hemisphere Map
            HStack(alignment: .top, spacing: 20) {
                // Circular Obstruction Radar Canvas
                VStack(alignment: .leading, spacing: 10) {
                    HStack {
                        Text("CELESTIAL OBSTRUCTION RADAR (101×101 SNR GRID)")
                            .font(.system(size: 10, weight: .bold))
                            .foregroundStyle(.secondary)
                        Spacer()
                        Text("360° AZIMUTH")
                            .font(.system(size: 9, weight: .semibold))
                            .foregroundStyle(.cyan)
                    }

                    ZStack {
                        RoundedRectangle(cornerRadius: 12)
                            .fill(Color.black.opacity(0.85))
                            .overlay(
                                RoundedRectangle(cornerRadius: 12)
                                    .stroke(Color.cyan.opacity(0.2), lineWidth: 1)
                            )

                        ObstructionRadarCanvas()
                            .frame(height: 280)
                            .padding(14)
                    }
                    .frame(height: 280)
                }
                .frame(maxWidth: .infinity)

                // Satellite Tracking & Constellation Details
                VStack(alignment: .leading, spacing: 12) {
                    Text("CONSTELLATION & TRACKING STATUS")
                        .font(.system(size: 10, weight: .bold))
                        .foregroundStyle(.secondary)

                    VStack(spacing: 8) {
                        ConstellationRow(label: "Device State", value: "CONNECTED", isGood: true)
                        ConstellationRow(label: "Dish Hardware", value: "rev3_proto2", isGood: true)
                        ConstellationRow(label: "Boresight Azimuth", value: "12.4° N", isGood: true)
                        ConstellationRow(label: "Boresight Elevation", value: "68.2°", isGood: true)
                        ConstellationRow(label: "Ping Latency (Gateway)", value: "28 ms", isGood: true)
                        ConstellationRow(label: "Ping Drop Rate", value: "0.0 %", isGood: true)
                        ConstellationRow(label: "Active Satellites in View", value: "14 Starlink V2 Mini", isGood: true)
                        ConstellationRow(label: "Obstruction Duration", value: "0 sec (Past 12h)", isGood: true)
                    }
                    .padding(12)
                    .background(Color(nsColor: .controlBackgroundColor).opacity(0.6))
                    .clipShape(RoundedRectangle(cornerRadius: 10))
                }
                .frame(width: 320)
            }
        }
        .padding(18)
        .background(Color(nsColor: .windowBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 14))
        .shadow(color: Color.black.opacity(0.04), radius: 4, x: 0, y: 1)
        .confirmationDialog(dialogAction.rawValue, isPresented: $showConfirmDialog) {
            Button(dialogAction.rawValue, role: dialogAction == .stow ? .destructive : .none) {
                // Execute dish command
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Are you sure you want to execute \(dialogAction.rawValue) on the Starlink terminal?")
        }
    }
}

struct ObstructionRadarCanvas: View {
    var body: some View {
        Canvas { context, size in
            let center = CGPoint(x: size.width / 2, y: size.height / 2)
            let radius = min(size.width, size.height) / 2 - 10

            // Concentric elevation rings (30°, 60°, 90°)
            for ring in [0.33, 0.66, 1.0] {
                let r = radius * ring
                let rect = CGRect(x: center.x - r, y: center.y - r, width: r * 2, height: r * 2)
                context.stroke(
                    Path(ellipseIn: rect),
                    with: .color(Color.cyan.opacity(0.2)),
                    lineWidth: 1
                )
            }

            // Crosshairs
            var crossPath = Path()
            crossPath.move(to: CGPoint(x: center.x - radius, y: center.y))
            crossPath.addLine(to: CGPoint(x: center.x + radius, y: center.y))
            crossPath.move(to: CGPoint(x: center.x, y: center.y - radius))
            crossPath.addLine(to: CGPoint(x: center.x, y: center.y + radius))
            context.stroke(crossPath, with: .color(Color.cyan.opacity(0.15)), lineWidth: 1)

            // Simulated Clear Sky SNR Gradient Fill
            let radarCircle = Path(ellipseIn: CGRect(x: center.x - radius, y: center.y - radius, width: radius * 2, height: radius * 2))
            context.fill(
                radarCircle,
                with: .radialGradient(
                    Gradient(colors: [Color.green.opacity(0.35), Color.cyan.opacity(0.2), Color.indigo.opacity(0.1)]),
                    center: center,
                    startRadius: 0,
                    endRadius: radius
                )
            )

            // Live Satellite Tracking Dots
            let satCoords: [(Double, Double)] = [
                (0.2, 0.3), (-0.4, 0.5), (0.6, -0.2), (-0.3, -0.6), (0.1, -0.7),
                (0.7, 0.4), (-0.6, -0.1), (0.4, 0.7), (-0.2, 0.8)
            ]

            for (sx, sy) in satCoords {
                let px = center.x + CGFloat(sx) * radius
                let py = center.y + CGFloat(sy) * radius
                let satRect = CGRect(x: px - 3, y: py - 3, width: 6, height: 6)
                context.fill(Path(ellipseIn: satRect), with: .color(Color.green))
            }
        }
    }
}

struct DishMetricCard: View {
    let title: String
    let value: String
    let unit: String
    let icon: String
    let color: Color
    let badge: String

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Image(systemName: icon)
                    .font(.caption2)
                    .foregroundStyle(color)
                Text(title)
                    .font(.system(size: 8.5, weight: .bold))
                    .foregroundStyle(.secondary)
            }

            HStack(alignment: .firstTextBaseline, spacing: 3) {
                Text(value)
                    .font(.system(size: 20, weight: .heavy, design: .rounded))
                    .foregroundStyle(.primary)
                Text(unit)
                    .font(.system(size: 10, weight: .semibold))
                    .foregroundStyle(.secondary)
            }

            Text(badge)
                .font(.system(size: 8, weight: .bold))
                .foregroundStyle(color)
                .padding(.horizontal, 5)
                .padding(.vertical, 1.5)
                .background(color.opacity(0.12))
                .clipShape(Capsule())
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(nsColor: .controlBackgroundColor).opacity(0.5))
        .clipShape(RoundedRectangle(cornerRadius: 10))
    }
}

struct ConstellationRow: View {
    let label: String
    let value: String
    let isGood: Bool

    var body: some View {
        HStack {
            Text(label)
                .font(.caption2)
                .foregroundStyle(.secondary)
            Spacer()
            Text(value)
                .font(.caption2.weight(.semibold))
                .foregroundStyle(isGood ? Color.primary : Color.orange)
        }
    }
}
