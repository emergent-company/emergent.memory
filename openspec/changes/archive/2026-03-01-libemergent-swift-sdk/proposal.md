## Why

We need an SDK for Swift applications to interact seamlessly with our services. Since we have an existing, fully-featured Go SDK, creating a Swift SDK that acts as a bridge to the Go SDK (likely via CGO) will ensure feature parity, reduce code duplication, and accelerate the development of the Swift client.

## What Changes

- Introduction of `libemergent-swift-sdk`, a native Swift package for client applications.
- Implementation of a C/CGO bridge layer to expose the existing Go SDK functionality to Swift.
- Potential minor modifications to the Go SDK to ensure C-compatible exports are available.

## Capabilities

### New Capabilities
- `swift-sdk-bridge`: The CGO/C-interface bridge that wraps the Go SDK for consumption by Swift.
- `swift-sdk-client`: The native Swift client implementation that provides ergonomic, Swift-idiomatic APIs (async/await, structs, etc.) on top of the C bridge.

### Modified Capabilities

## Impact

- **Swift Clients**: Gain a native SDK to interact with our backend.
- **Go SDK**: May require exporting a C-compatible API surface.
- **Build System**: Will need tooling to compile Go to a C static/dynamic library (`.a` or `.xcframework`) and integrate it with Swift Package Manager (SPM).
