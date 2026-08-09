import SwiftUI
import StarlinkWiFiCore

public enum NavigationSection: String, CaseIterable, Identifiable {
    case overview = "Overview"
    case devices = "Devices"
    case wifi = "Wi-Fi"
    case metrics = "Metrics"
    case updates = "Updates"
    case settings = "Settings"

    public var id: String { rawValue }

    public var iconName: String {
        switch self {
        case .overview: return "house.fill"
        case .devices: return "cpu.fill"
        case .wifi: return "wifi"
        case .metrics: return "chart.xyaxis.line"
        case .updates: return "arrow.triangle.2.circlepath"
        case .settings: return "gearshape.fill"
        }
    }
}

public struct LinkPortSidebarView: View {
    @Binding var selection: NavigationSection

    public init(selection: Binding<NavigationSection>) {
        self._selection = selection
    }

    private func safeLoadBlackholeLogo() -> NSImage? {
        let fileManager = FileManager.default

        // 1. Check direct main bundle resources
        if let mainResPath = Bundle.main.path(forResource: "blackhole_logo", ofType: "jpg") {
            return NSImage(contentsOfFile: mainResPath)
        }

        // 2. Check Contents/Resources/blackhole_logo.jpg inside app bundle
        let bundlePath = Bundle.main.bundlePath
        let resPath = (bundlePath as NSString).appendingPathComponent("Contents/Resources/blackhole_logo.jpg")
        if fileManager.fileExists(atPath: resPath) {
            return NSImage(contentsOfFile: resPath)
        }

        // 3. Check inside resource bundle if packaged
        let subBundlePath = (bundlePath as NSString).appendingPathComponent("Contents/Resources/UniversalWiFiManager_StarlinkWiFiApp.bundle/blackhole_logo.jpg")
        if fileManager.fileExists(atPath: subBundlePath) {
            return NSImage(contentsOfFile: subBundlePath)
        }

        // 4. Fallback relative path for local development
        if fileManager.fileExists(atPath: "Sources/StarlinkWiFiApp/Resources/blackhole_logo.jpg") {
            return NSImage(contentsOfFile: "Sources/StarlinkWiFiApp/Resources/blackhole_logo.jpg")
        }

        return nil
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            // Brand Title Bar
            HStack(spacing: 10) {
                ZStack {
                    RoundedRectangle(cornerRadius: 8)
                        .fill(Color.black)
                        .frame(width: 32, height: 32)
                    
                    if let nsImage = safeLoadBlackholeLogo() {
                        Image(nsImage: nsImage)
                            .resizable()
                            .scaledToFill()
                            .clipShape(RoundedRectangle(cornerRadius: 8))
                            .frame(width: 32, height: 32)
                    } else {
                        ZStack {
                            Circle()
                                .fill(LinearGradient(colors: [.purple, .blue, .black], startPoint: .topLeading, endPoint: .bottomTrailing))
                            Circle()
                                .stroke(Color.cyan, lineWidth: 1.5)
                            Circle()
                                .fill(Color.black)
                                .scaleEffect(0.4)
                        }
                        .frame(width: 28, height: 28)
                    }
                }

                VStack(alignment: .leading, spacing: 1) {
                    Text("Event Horizon")
                        .font(.headline.weight(.bold))
                    Text("USB Network Suite")
                        .font(.system(size: 9, weight: .medium))
                        .foregroundStyle(.secondary)
                }
            }
            .padding(.horizontal, 14)
            .padding(.top, 16)

            Divider()
                .padding(.horizontal, 10)

            // Navigation Items
            VStack(spacing: 3) {
                ForEach(NavigationSection.allCases) { section in
                    let isSelected = selection == section
                    Button(action: {
                        withAnimation(.spring(response: 0.25, dampingFraction: 0.8)) {
                            selection = section
                        }
                    }) {
                        HStack(spacing: 10) {
                            Image(systemName: section.iconName)
                                .font(.body)
                                .foregroundStyle(isSelected ? Color.blue : Color.primary.opacity(0.8))
                                .frame(width: 20)

                            Text(section.rawValue)
                                .font(.body.weight(isSelected ? .semibold : .regular))

                            Spacer()

                            if section == .updates {
                                Circle()
                                    .fill(Color.orange)
                                    .frame(width: 6, height: 6)
                            }
                        }
                        .padding(.horizontal, 12)
                        .padding(.vertical, 8)
                        .background(
                            RoundedRectangle(cornerRadius: 8)
                                .fill(isSelected ? Color.blue.opacity(0.12) : Color.clear)
                        )
                        .foregroundStyle(isSelected ? Color.blue : Color.primary)
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal, 8)

            Spacer()

            // Footer System Status Pill
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 8) {
                    Circle()
                        .fill(Color.green)
                        .frame(width: 7, height: 7)
                        .shadow(color: Color.green.opacity(0.6), radius: 3)

                    Text("Daemon Connected")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(.secondary)

                    Spacer()

                    Text("v1.0.0")
                        .font(.system(size: 9, weight: .bold).monospaced())
                        .foregroundStyle(.tertiary)
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .background(Color.secondary.opacity(0.06))
                .clipShape(RoundedRectangle(cornerRadius: 6))
            }
            .padding(.horizontal, 10)
            .padding(.bottom, 14)
        }
        .frame(width: 210)
        .background(Color(nsColor: .windowBackgroundColor))
    }
}
