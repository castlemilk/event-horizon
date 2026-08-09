import SwiftUI

public struct LiveThroughputChartView: View {
    let rxData: [Double]
    let txData: [Double]

    public init(rxData: [Double] = [], txData: [Double] = []) {
        self.rxData = rxData.isEmpty ? [120, 140, 130, 180, 210, 190, 240, 220, 312] : rxData
        self.txData = txData.isEmpty ? [30, 45, 40, 50, 60, 55, 62, 58, 68] : txData
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("Throughput History (24-hour)")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)

                Spacer()

                HStack(spacing: 12) {
                    HStack(spacing: 4) {
                        Circle()
                            .fill(Color.blue)
                            .frame(width: 6, height: 6)
                        Text("Download")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }

                    HStack(spacing: 4) {
                        Circle()
                            .fill(Color.teal)
                            .frame(width: 6, height: 6)
                        Text("Upload")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }
            }

            ZStack(alignment: .bottomLeading) {
                // Background Grid Lines
                VStack(spacing: 0) {
                    Divider()
                    Spacer()
                    Divider()
                    Spacer()
                    Divider()
                }
                .foregroundStyle(Color.secondary.opacity(0.08))

                // Download Gradient Area & Line
                LineGraphAreaShape(data: rxData, maxVal: 400)
                    .fill(
                        LinearGradient(
                            colors: [Color.blue.opacity(0.18), Color.blue.opacity(0.0)],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                    )

                LineGraphShape(data: rxData, maxVal: 400)
                    .stroke(Color.blue, style: StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))

                // Upload Gradient Area & Line
                LineGraphAreaShape(data: txData, maxVal: 400)
                    .fill(
                        LinearGradient(
                            colors: [Color.teal.opacity(0.15), Color.teal.opacity(0.0)],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                    )

                LineGraphShape(data: txData, maxVal: 400)
                    .stroke(Color.teal, style: StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
            }
            .frame(height: 80)

            // Time X-Axis
            HStack {
                Text("24h ago")
                Spacer()
                Text("18h")
                Spacer()
                Text("12h")
                Spacer()
                Text("6h")
                Spacer()
                Text("Live")
            }
            .font(.system(size: 9, weight: .medium).monospacedDigit())
            .foregroundStyle(.tertiary)
        }
    }
}

public struct LineGraphShape: Shape {
    public let data: [Double]
    public let minVal: Double
    public let maxVal: Double

    public init(data: [Double], minVal: Double = 0.0, maxVal: Double = 100.0) {
        self.data = data
        self.minVal = minVal
        self.maxVal = maxVal
    }

    public func path(in rect: CGRect) -> Path {
        var path = Path()
        guard data.count > 1 else { return path }

        let minV = minVal
        let maxV = max(maxVal, minV + 0.001)
        let range = maxV - minV
        let stepX = rect.width / CGFloat(data.count - 1)
        let padding: CGFloat = 4.0
        let drawHeight = max(rect.height - (padding * 2), 1.0)

        for i in 0..<data.count {
            let x = CGFloat(i) * stepX
            let normalized = CGFloat((data[i] - minV) / range)
            let clampedNorm = max(0.0, min(1.0, normalized))
            let y = rect.height - padding - (clampedNorm * drawHeight)

            if i == 0 {
                path.move(to: CGPoint(x: x, y: y))
            } else {
                path.addLine(to: CGPoint(x: x, y: y))
            }
        }
        return path
    }
}

public struct LineGraphAreaShape: Shape {
    public let data: [Double]
    public let minVal: Double
    public let maxVal: Double

    public init(data: [Double], minVal: Double = 0.0, maxVal: Double = 100.0) {
        self.data = data
        self.minVal = minVal
        self.maxVal = maxVal
    }

    public func path(in rect: CGRect) -> Path {
        var path = Path()
        guard data.count > 1 else { return path }

        let minV = minVal
        let maxV = max(maxVal, minV + 0.001)
        let range = maxV - minV
        let stepX = rect.width / CGFloat(data.count - 1)
        let padding: CGFloat = 4.0
        let drawHeight = max(rect.height - (padding * 2), 1.0)

        path.move(to: CGPoint(x: 0, y: rect.height))

        for i in 0..<data.count {
            let x = CGFloat(i) * stepX
            let normalized = CGFloat((data[i] - minV) / range)
            let clampedNorm = max(0.0, min(1.0, normalized))
            let y = rect.height - padding - (clampedNorm * drawHeight)
            path.addLine(to: CGPoint(x: x, y: y))
        }

        path.addLine(to: CGPoint(x: rect.width, y: rect.height))
        path.closeSubpath()
        return path
    }
}
