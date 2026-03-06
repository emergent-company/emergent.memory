// swift-tools-version: 5.9
// Package.swift for EmergentKit — the Swift SDK for the Emergent platform.
//
// Development / local build:
//   The XCFramework binary target is referenced locally.  Run the build
//   script first to generate it:
//
//     task server:swift:xcframework
//     # or
//     apps/server-go/scripts/build-xcframework.sh
//
// Release / SPM consumers:
//   Switch the `EmergentGoCore` target below from `.binaryTarget(path:…)` to
//   `.binaryTarget(url:…, checksum:…)` pointing at the GitHub release asset.
//   See scripts/release-xcframework.sh for automation.

import PackageDescription

let package = Package(
    name: "EmergentKit",

    // -----------------------------------------------------------------------
    // Minimum platform requirements (Swift 6 Concurrency, OSLog, etc.)
    // -----------------------------------------------------------------------
    platforms: [
        .iOS(.v15),
        .macOS(.v12),
    ],

    // -----------------------------------------------------------------------
    // Public products
    // -----------------------------------------------------------------------
    products: [
        .library(
            name: "EmergentKit",
            targets: ["EmergentKit"]
        ),
    ],

    // -----------------------------------------------------------------------
    // Targets
    // -----------------------------------------------------------------------
    targets: [
        // ------------------------------------------------------------------
        // Binary XCFramework containing the compiled Go C-bridge.
        // Switch to the `url:` / `checksum:` variant for SPM distribution.
        // ------------------------------------------------------------------
        .binaryTarget(
            name: "EmergentGoCore",
            path: "../../apps/server-go/dist/swift/EmergentGoCore.xcframework"
        ),

        // ------------------------------------------------------------------
        // Internal bridging module — wraps the raw C symbols from the
        // XCFramework and keeps them out of the public API namespace.
        // ------------------------------------------------------------------
        .target(
            name: "EmergentBridge",
            dependencies: ["EmergentGoCore"],
            path: "Sources/EmergentBridge",
            // Suppress deprecation warnings coming from Go-generated headers.
            swiftSettings: [
                .unsafeFlags(["-suppress-warnings"]),
            ]
        ),

        // ------------------------------------------------------------------
        // Public Swift SDK
        // ------------------------------------------------------------------
        .target(
            name: "EmergentKit",
            dependencies: ["EmergentBridge"],
            path: "Sources/EmergentKit",
            swiftSettings: [
                // Enable full Swift 6 strict concurrency checking.
                .enableExperimentalFeature("StrictConcurrency"),
            ]
        ),

        // ------------------------------------------------------------------
        // Swift unit tests — run without the XCFramework via a mock bridge.
        // ------------------------------------------------------------------
        .testTarget(
            name: "EmergentKitTests",
            dependencies: ["EmergentKit"],
            path: "Tests/EmergentKitTests"
        ),

        // ------------------------------------------------------------------
        // Integration / E2E tests — require a running server + XCFramework.
        // ------------------------------------------------------------------
        .testTarget(
            name: "EmergentKitIntegrationTests",
            dependencies: ["EmergentKit", "EmergentBridge"],
            path: "Tests/EmergentKitIntegrationTests"
        ),
    ]
)
