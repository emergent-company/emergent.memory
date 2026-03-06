## Context

We want to provide a Swift SDK (`libemergent-swift-sdk`) to allow native iOS and macOS applications to interact with our backend services. Rather than rewriting the entire business logic, API clients, and data models natively in Swift, we intend to leverage our existing, fully-featured Go SDK. By using CGO, we can compile the Go SDK into a C-compatible library, which can then be wrapped by Swift.

## Goals / Non-Goals

**Goals:**
- Provide a Swift-idiomatic, ergonomic API using modern Swift features (`async`/`await`, `Codable`, `structs`, `enums`).
- Leverage the existing Go SDK implementation to ensure 100% feature parity.
- Package the SDK using Swift Package Manager (SPM) via a precompiled `XCFramework`.

**Non-Goals:**
- Rewriting the core SDK logic, networking, or state management natively in Swift.
- Supporting legacy Swift versions (we will target modern Swift with Concurrency).

## Decisions

### 1. Architecture Strategy: Go -> CGO -> Swift Interop -> Idiomatic Swift
- **Rationale**: To minimize duplication, we will create a small CGO adapter layer in Go that exports C-compatible functions (e.g., using `//export` and `C` types like `*C.char`). We will then generate a C header file. A low-level Swift module will interact with this C API, and a higher-level Swift module will provide the idiomatic `async`/`await` and strongly typed interfaces.
- **Alternatives**: Using protocol buffers/gRPC natively, or rewriting the SDK. Rewriting takes too much effort, and gRPC might not cover local state or custom offline logic present in the SDK.

### 2. Packaging: XCFramework
- **Rationale**: Apple ecosystem projects heavily rely on SPM. Since we cannot easily compile Go code during an SPM build, we must distribute a precompiled binary. An `XCFramework` allows us to bundle static libraries (`.a`) for multiple platforms (macOS arm64/x86_64, iOS arm64, iOS Simulator arm64/x86_64) into a single artifact that SPM can easily consume.
- **Alternatives**: Distributing raw `.a` files (too complex for consumers to set up search paths) or relying on CocoaPods (SPM is the modern standard).

### 3. Memory Management & Data Passing
- **Rationale**: CGO requires explicit memory management. For complex types, we will serialize Go structs to JSON strings (`*C.char`), pass them across the boundary, and parse them in Swift using `Codable`. For memory, if Go allocates a string to pass to Swift, Swift must call a corresponding Go-exported `FreeString` function to avoid leaks.
- **Alternatives**: Creating complex C-struct layouts. This is brittle and hard to maintain compared to JSON serialization across the boundary.

### 4. Asynchrony
- **Rationale**: Go's concurrency model (goroutines) doesn't map directly to Swift Concurrency. We will export callback-based C functions (taking function pointers) from Go. The Swift layer will use `withCheckedContinuation` or `withCheckedThrowingContinuation` to wrap these callbacks into modern `async` functions.

### 5. Testing Strategy
- **Go Bridge Tests**: We will write native Go tests (`_test.go`) against the exported CGO functions to ensure JSON serialization, memory freeing logic, and context cancellation mapping work correctly before they ever reach C or Swift.
- **Swift Unit Tests (`XCTest`)**: We will use a protocol/mocking approach internally within the Swift Package so that Swift unit tests can verify JSON decoding, `EmergentError` mapping, and actor state management *without* requiring the compiled Go C-library to be present during standard test runs.
- **End-to-End Build & Integration Tests**: We will create a small automated script that compiles the actual `XCFramework`, links it into an XCTest target, and runs a live integration test to verify the full Go -> C -> Swift pipeline.

### 6. Distribution Strategy: Remote Binary Targets
- **Rationale**: To prevent bloating the Git repository with large precompiled `XCFramework` binaries, we will distribute the SDK using remote binary targets. Each release will involve zipping the `XCFramework`, uploading it to a stable host (e.g., GitHub Releases), and updating the `Package.swift` with the remote URL and a SHA256 checksum.
- **Alternatives**: Committing the binary to the repo (causes repo bloat) or using a separate binary-only repository (adds maintenance overhead).

## Risks / Trade-offs

- **[Risk] Increased App Binary Size:** Go binaries include a runtime, which can significantly increase the final app size.
  - **Mitigation:** Use linker flags `-ldflags="-s -w"` to strip debugging information. Evaluate the final size impact on dummy applications.
- **[Risk] Cross-Compilation Complexity:** Building an `XCFramework` for all Apple targets requires a complex build script involving `CGO_ENABLED=1`, various `GOOS`/`GOARCH` targets, and the `xcodebuild -create-xcframework` command.
  - **Mitigation:** Create an automated script (or Taskfile) to handle the complete build pipeline and generate the `XCFramework` deterministically.
- **[Risk] Memory Leaks across CGO boundary:** Mishandling pointers between Go and Swift can lead to memory leaks or crashes.
  - **Mitigation:** Rely on a clear convention: use JSON strings for complex data, and provide specific `Free()` functions exported from Go for any memory Go allocates and hands to C/Swift.
