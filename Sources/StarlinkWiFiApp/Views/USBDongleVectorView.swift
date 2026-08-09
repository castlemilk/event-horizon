import SwiftUI

public struct USBDongleVectorView: View {
    public init() {}

    public var body: some View {
        ZStack {
            // Shadow behind USB dongle
            RoundedRectangle(cornerRadius: 14)
                .fill(Color.black.opacity(0.15))
                .frame(width: 80, height: 140)
                .blur(radius: 12)
                .offset(x: 8, y: 10)

            // USB Dongle Body
            VStack(spacing: 0) {
                // USB Type-A Plug (Metallic Top)
                ZStack {
                    RoundedRectangle(cornerRadius: 3)
                        .fill(
                            LinearGradient(
                                colors: [Color(white: 0.85), Color(white: 0.70), Color(white: 0.90)],
                                startPoint: .leading,
                                endPoint: .trailing
                            )
                        )
                        .frame(width: 44, height: 38)
                        .overlay(
                            RoundedRectangle(cornerRadius: 3)
                                .stroke(Color(white: 0.6), lineWidth: 1)
                        )

                    // USB Pin Holes
                    HStack(spacing: 8) {
                        Rectangle()
                            .fill(Color.black.opacity(0.7))
                            .frame(width: 8, height: 12)
                        Rectangle()
                            .fill(Color.black.opacity(0.7))
                            .frame(width: 8, height: 12)
                    }
                }

                // Plastic Dongle Body
                ZStack {
                    RoundedRectangle(cornerRadius: 12)
                        .fill(
                            LinearGradient(
                                colors: [Color(white: 0.22), Color(white: 0.12)],
                                startPoint: .topLeading,
                                endPoint: .bottomTrailing
                            )
                        )
                        .frame(width: 90, height: 135)
                        .overlay(
                            RoundedRectangle(cornerRadius: 12)
                                .stroke(Color.white.opacity(0.15), lineWidth: 1)
                        )

                    VStack(spacing: 16) {
                        // Wi-Fi Waves Symbol on Dongle Body
                        Image(systemName: "wifi")
                            .font(.system(size: 28, weight: .bold))
                            .foregroundStyle(Color.white.opacity(0.3))

                        Spacer()

                        // LED Indicator Light
                        Capsule()
                            .fill(Color.cyan)
                            .frame(width: 4, height: 14)
                            .shadow(color: Color.cyan, radius: 4)
                    }
                    .padding(.vertical, 20)
                }
            }
        }
        .frame(width: 130, height: 180)
    }
}
