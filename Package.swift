// swift-tools-version: 5.10
import PackageDescription

let package = Package(
    name: "UniversalWiFiManager",
    platforms: [
        .macOS(.v14)
    ],
    products: [
        .library(
            name: "EventHorizonCore",
            targets: ["EventHorizonCore"]
        ),
        .executable(
            name: "EventHorizonApp",
            targets: ["EventHorizonApp"]
        )
    ],
    dependencies: [],
    targets: [
        .target(
            name: "EventHorizonCore",
            dependencies: [],
            swiftSettings: [
                .enableUpcomingFeature("StrictConcurrency")
            ]
        ),
        .executableTarget(
            name: "EventHorizonApp",
            dependencies: ["EventHorizonCore"],
            resources: [
                .process("Resources")
            ],
            swiftSettings: [
                .enableUpcomingFeature("StrictConcurrency")
            ]
        ),
        .testTarget(
            name: "EventHorizonTests",
            dependencies: ["EventHorizonCore", "EventHorizonApp"],
            swiftSettings: [
                .enableUpcomingFeature("StrictConcurrency")
            ]
        )
    ]
)
