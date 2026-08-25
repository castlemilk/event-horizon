import SwiftUI
import EventHorizonCore

public struct MultiInterfaceRoutingView: View {
    @Bindable var store: WiFiManagerStore

    public init(store: WiFiManagerStore) {
        self.store = store
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            // Header with Policy Status & Refresh
            HStack {
                HStack(spacing: 8) {
                    Image(systemName: "arrow.triangle.swap")
                        .font(.title2)
                        .foregroundStyle(.purple)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Multi-WAN & Policy Routing")
                            .font(.headline)
                        Text("Default gateway priority & automated link failover")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }

                Spacer()

                Button {
                    Task {
                        await store.fetchRoutingPolicy()
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

            // Auto-Failover Toggle Card
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Automated Link Failover")
                        .font(.subheadline.weight(.semibold))
                    Text("Auto-switches default route if primary gateway drops pings")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Toggle("", isOn: Binding(
                    get: { store.routingPolicy?.autoFailoverEnabled ?? true },
                    set: { newVal in
                        Task {
                            await store.setAutoFailover(enabled: newVal)
                        }
                    }
                ))
                .toggleStyle(.switch)
            }
            .padding(12)
            .background(Color.purple.opacity(0.08))
            .clipShape(RoundedRectangle(cornerRadius: 10))

            // Interface Route Table
            VStack(alignment: .leading, spacing: 8) {
                Text("NETWORK INTERFACES & ROUTE PRIORITY")
                    .font(.system(size: 10, weight: .bold))
                    .foregroundStyle(.secondary)

                let ifaces = store.routingPolicy?.interfaces ?? []
                if ifaces.isEmpty {
                    Text("Discovering active routes...")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .padding(.vertical, 8)
                } else {
                    ForEach(ifaces) { iface in
                        HStack(spacing: 12) {
                            Circle()
                                .fill(iface.isReachable ? Color.green : Color.orange)
                                .frame(width: 8, height: 8)

                            VStack(alignment: .leading, spacing: 2) {
                                HStack(spacing: 6) {
                                    Text(iface.name)
                                        .font(.callout.weight(.bold))
                                    if iface.isDefault {
                                        Text("DEFAULT ROUTE")
                                            .font(.system(size: 8, weight: .bold))
                                            .foregroundStyle(.green)
                                            .padding(.horizontal, 6)
                                            .padding(.vertical, 2)
                                            .background(Color.green.opacity(0.15))
                                            .clipShape(Capsule())
                                    }
                                }
                                Text("\(iface.description) • IP: \(iface.ip)")
                                    .font(.caption2)
                                    .foregroundStyle(.secondary)
                            }

                            Spacer()

                            if !iface.isDefault {
                                Button("Set Default") {
                                    Task {
                                        await store.setDefaultRoute(interface: iface.name)
                                    }
                                }
                                .font(.caption.weight(.semibold))
                                .buttonStyle(.borderedProminent)
                                .controlSize(.small)
                            } else {
                                Text("Active")
                                    .font(.caption.weight(.semibold))
                                    .foregroundStyle(.green)
                            }
                        }
                        .padding(10)
                        .background(Color(nsColor: .controlBackgroundColor).opacity(0.6))
                        .clipShape(RoundedRectangle(cornerRadius: 8))
                    }
                }
            }

            // Recent Failover Events
            if let events = store.routingPolicy?.recentEvents, !events.isEmpty {
                VStack(alignment: .leading, spacing: 6) {
                    Text("RECENT FAILOVER EVENTS")
                        .font(.system(size: 9, weight: .bold))
                        .foregroundStyle(.secondary)

                    ForEach(events.prefix(3)) { evt in
                        HStack {
                            Image(systemName: "clock.arrow.circlepath")
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                            Text("\(evt.fromIface) ➔ \(evt.toIface): \(evt.reason)")
                                .font(.caption2)
                            Spacer()
                        }
                        .padding(6)
                        .background(Color.secondary.opacity(0.08))
                        .clipShape(RoundedRectangle(cornerRadius: 6))
                    }
                }
            }
        }
        .padding(14)
        .background(Color(nsColor: .windowBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 12))
        .shadow(color: Color.black.opacity(0.04), radius: 3, x: 0, y: 1)
        .task {
            if store.routingPolicy == nil {
                await store.fetchRoutingPolicy()
            }
        }
    }
}
