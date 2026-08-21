import SwiftUI
import EventHorizonCore

public struct EndpointListView: View {
    let hotspots: [AccessPoint]
    let onSelect: (AccessPoint) -> Void

    @State private var searchText = ""

    public init(hotspots: [AccessPoint], onSelect: @escaping (AccessPoint) -> Void) {
        self.hotspots = hotspots
        self.onSelect = onSelect
    }

    private var filteredHotspots: [AccessPoint] {
        if searchText.isEmpty {
            return hotspots
        }
        return hotspots.filter { $0.ssid.localizedCaseInsensitiveContains(searchText) }
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            // Header Bar
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Available Networks")
                        .font(.title2.weight(.bold))
                    Text("In-range 802.11 Wi-Fi access points scanned by network interfaces")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }

                Spacer()

                HStack(spacing: 6) {
                    Image(systemName: "magnifyingglass")
                        .foregroundStyle(.secondary)
                    TextField("Search networks...", text: $searchText)
                        .textFieldStyle(.plain)
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .background(Color(nsColor: .controlBackgroundColor))
                .clipShape(RoundedRectangle(cornerRadius: 6))
                .overlay(
                    RoundedRectangle(cornerRadius: 6)
                        .stroke(Color.secondary.opacity(0.15), lineWidth: 1)
                )
                .frame(width: 210)
            }

            // Networks List Container
            VStack(spacing: 0) {
                ForEach(filteredHotspots) { ap in
                    NetworkListRow(ap: ap, onConnect: { onSelect(ap) })
                    if ap.id != filteredHotspots.last?.id {
                        Divider()
                            .padding(.leading, 48)
                    }
                }

                if filteredHotspots.isEmpty {
                    VStack(spacing: 8) {
                        Image(systemName: "wifi.slash")
                            .font(.largeTitle)
                            .foregroundStyle(.secondary)
                        Text("No Wi-Fi networks found.")
                            .font(.body.weight(.medium))
                            .foregroundStyle(.secondary)
                    }
                    .padding(30)
                    .frame(maxWidth: .infinity)
                }
            }
            .background(Color(nsColor: .windowBackgroundColor))
            .clipShape(RoundedRectangle(cornerRadius: 10))
            .overlay(
                RoundedRectangle(cornerRadius: 10)
                    .stroke(Color.secondary.opacity(0.12), lineWidth: 1)
            )

            // Bottom Action Bar
            HStack {
                Button(action: {}) {
                    Label("Add Network...", systemImage: "plus")
                }
                .buttonStyle(.bordered)

                Spacer()

                Button("Other Options...") {}
                    .buttonStyle(.bordered)
                    .foregroundStyle(.secondary)

                Button(action: {}) {
                    Image(systemName: "questionmark.circle")
                }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
            }
        }
    }
}

struct NetworkListRow: View {
    let ap: AccessPoint
    let onConnect: () -> Void

    var body: some View {
        HStack(spacing: 14) {
            Image(systemName: "wifi")
                .font(.title3)
                .foregroundStyle(ap.isSelected ? Color.green : Color.primary.opacity(0.85))
                .frame(width: 24)

            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(ap.ssid)
                        .font(.body.weight(.semibold))

                    if ap.isSelected {
                        Text("Connected")
                            .font(.caption2.weight(.bold))
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Color.green.opacity(0.15))
                            .foregroundStyle(.green)
                            .clipShape(Capsule())
                    }
                }

                Text("\(ap.security) • \(ap.channel > 14 ? "5 GHz" : "2.4 GHz") (Channel \(ap.channel))")
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
            }

            Spacer()

            if ap.security != "Open" {
                Image(systemName: "lock.fill")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }

            SignalMeterView(rssi: ap.rssi)

            Button(action: onConnect) {
                Text(ap.isSelected ? "Connected" : "Connect")
                    .font(.caption.weight(.semibold))
            }
            .buttonStyle(.borderedProminent)
            .tint(ap.isSelected ? .green : .blue)
            .disabled(ap.isSelected)
            .frame(width: 90)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
    }
}

struct SignalMeterView: View {
    let rssi: Int8

    var body: some View {
        HStack(alignment: .bottom, spacing: 2) {
            Capsule()
                .fill(rssi > -90 ? Color.green : Color.secondary.opacity(0.25))
                .frame(width: 3, height: 6)
            Capsule()
                .fill(rssi > -75 ? Color.green : Color.secondary.opacity(0.25))
                .frame(width: 3, height: 10)
            Capsule()
                .fill(rssi > -60 ? Color.green : Color.secondary.opacity(0.25))
                .frame(width: 3, height: 14)
        }
        .padding(.horizontal, 6)
    }
}
