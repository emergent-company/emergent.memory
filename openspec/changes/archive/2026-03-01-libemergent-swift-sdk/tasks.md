## 1. Build Script & Infrastructure Setup

- [x] 1.1 Create build script to compile Go codebase into C static libraries (`.a`) for iOS (arm64), iOS Simulator (arm64, x86_64), and macOS (arm64, x86_64).
- [x] 1.2 Add step to build script to generate the C header file (`.h`) from Go CGO exports.
- [x] 1.3 Create build script to combine the `.a` files and headers into an `XCFramework` using `xcodebuild -create-xcframework`.
- [x] 1.4 Add Taskfile targets to automate the full XCFramework generation process deterministically.

## 2. Go SDK C-Bridge Implementation (Core)

- [x] 2.1 Create a new Go package (e.g., `cmd/swiftbridge` or `pkg/swiftbridge`) specifically for CGO exports.
- [x] 2.2 Implement and `//export` a `FreeString(*C.char)` function to allow Swift to free Go-allocated memory.
- [x] 2.3 Implement and `//export` a `FreeClient(uint32)` function to deallocate Go-side client state.
- [x] 2.4 Implement and `//export` the initialization function (e.g., `CreateClient`) that returns a numeric handle or pointer.
- [x] 2.5 Implement a basic "ping" or "health" `//export` endpoint that takes a simple JSON string and returns a JSON response synchronously to serve as our POC.

## 3. POC Integration & Testing (Tight Feedback Loop)

- [x] 3.1 Write native Go tests (`_test.go`) in `swiftbridge` verifying `CreateClient`, `FreeClient`, and the POC endpoint handle JSON correctly.
- [x] 3.2 Initialize a minimal Swift Package (`Package.swift`) locally referencing the generated `XCFramework`.
- [x] 3.3 Create a barebones Swift test target that imports the C-bridge, calls `CreateClient`, executes the POC endpoint, and calls `FreeString`/`FreeClient`.
- [x] 3.4 Run the Swift test to verify the CGO boundary, memory safety, and XCFramework linking work end-to-end before expanding the API.

## 4. Advanced C-Bridge Implementation (Async & Concurrency)

- [x] 4.1 Define C-compatible callback function pointer types in CGO to support async operations and logging.
- [x] 4.2 Implement an `//export` function to register a global log callback from Swift.
- [x] 4.3 Implement a safe map mechanism in Go to store `context.CancelFunc` by operation ID, and `//export` a `CancelOperation(uint64)` function.
- [x] 4.4 Implement and `//export` the full suite of core API endpoint functions using async callbacks, operation IDs, and standardized JSON errors.
- [x] 4.5 Expand Go tests to cover cancellation and async callback invocation.

## 5. Full Swift Client Implementation

- [x] 5.1 Define Swift data models and `Codable` structs that correspond to the JSON payloads expected by the Go C-Bridge.
- [x] 5.2 Ensure all public Swift data models explicitly conform to the `Sendable` protocol for Swift 6 strict concurrency support.
- [x] 5.3 Define an `EmergentError` enum to map Go-side errors to idiomatic Swift cases.
- [x] 5.4 Create a low-level internal C-interop utility to handle string conversions, ensuring `FreeString` via `defer`.
- [x] 5.5 Define a public `EmergentService` protocol outlining all SDK operations.
- [x] 5.6 Implement `EmergentClient` as an `actor` conforming to `EmergentService`.
- [x] 5.7 Implement the global log callback in Swift routing Go logs to `os.Logger`.
- [x] 5.8 Implement idiomatic Swift wrapper functions on the actor (`withCheckedThrowingContinuation`, `withTaskCancellationHandler`).
- [x] 5.9 Document all public types using DocC (`///`).

## 6. Comprehensive Swift Testing & Validation

- [x] 6.1 Implement an internal Swift protocol to mock the C-bridge, enabling fast Swift unit tests without the `XCFramework`.
- [x] 6.2 Write Swift `XCTest` cases verifying JSON decoding, `EmergentError` mapping, continuation resolution, and task cancellation logic against the mocked bridge.
- [x] 6.3 Update the POC integration test (from Phase 3) to run a full E2E test suite against the complete API and real `XCFramework`.

## 7. Distribution & Release Automation

- [x] 7.1 Automate the creation of a release ZIP containing the `XCFramework`.
- [x] 7.2 Add a step to calculate the SHA256 checksum of the release ZIP.
- [x] 7.3 Update `Package.swift` to use a remote `binaryTarget` pointing to the release URL with the correct checksum.
