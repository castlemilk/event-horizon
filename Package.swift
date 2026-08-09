// swift-tools-version: 5.10
import PackageDescription

let package = Package(
    name: "UniversalWiFiManager",
    platforms: [
        .macOS(.v14)
    ],
    products: [
        .library(
            name: "StarlinkWiFiCore",
            targets: ["StarlinkWiFiCore"]
        ),
        .executable(
            name: "StarlinkWiFiApp",
            targets: ["StarlinkWiFiApp"]
        )
    ],
    dependencies: [],
    targets: [
        .target(
            name: "StarlinkWiFiCore",
            dependencies: [],
            swiftSettings: [
                .enableUpcomingFeature("StrictConcurrency")
            ]
        ),
        .executableTarget(
            name: "StarlinkWiFiApp",
            dependencies: ["StarlinkWiFiCore"],
            resources: [
                .process("Resources")
            ],
            swiftSettings: [
                .enableUpcomingFeature("StrictConcurrency")
            ]
        ),
        .testTarget(
            name: "StarlinkWiFiTests",
            dependencies: ["StarlinkWiFiCore"],
            swiftSettings: [
                .enableUpcomingFeature("StrictConcurrency")
            ]
        )
    ]
)
