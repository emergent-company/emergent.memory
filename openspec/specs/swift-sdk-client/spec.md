## ADDED Requirements

### Requirement: Minimum Platform Support
The Swift SDK MUST target modern Apple OS versions that support native Swift Concurrency to ensure stability and performance.

#### Scenario: Defining platform constraints
- **WHEN** the `Package.swift` is configured
- **THEN** it explicitly specifies a minimum of iOS 15.0 and macOS 12.0

### Requirement: Swift-Idiomatic Interfaces
The Swift SDK MUST wrap the C-interface in strongly-typed Swift models using `Codable` for request and response JSON serialization across the boundary. All public data models MUST explicitly conform to the `Sendable` protocol to ensure thread-safety when crossing isolation domains.

#### Scenario: Crossing isolation domains
- **WHEN** a data model is passed to or returned from an `async` method on the `EmergentClient` actor
- **THEN** it satisfies Swift 6 strict concurrency checks due to its `Sendable` conformance

### Requirement: Actor-Based Lifecycle Management
The Swift SDK MUST provide a primary client type implemented as an `actor` (e.g., `EmergentClient`) to guarantee thread-safe access to the underlying Go-side client handle and local state, ensuring resources are freed safely.

#### Scenario: Safe state management
- **WHEN** multiple concurrent Swift tasks use the SDK client
- **THEN** the `actor` isolation prevents data races when accessing the underlying Go handle or generating operation IDs, and its `deinit` block calls `FreeClient()`

### Requirement: Protocol-Oriented Mocking
The Swift SDK MUST define a public protocol (e.g., `EmergentService`) that outlines all operations, allowing consuming applications to inject mock implementations for unit testing.

#### Scenario: Testing client applications
- **WHEN** a developer writes unit tests for their app
- **THEN** they can use a mock conforming to the SDK protocol without triggering the actual Go C-bridge

### Requirement: C-Symbol Encapsulation
The raw C-bridge symbols MUST NOT leak into the public namespace of the consuming application. The Swift package must encapsulate the C API via module configuration or strict access control.

#### Scenario: Auto-completing in Xcode
- **WHEN** a developer types in their application code
- **THEN** they only see the idiomatic Swift APIs and not the underlying internal C functions

### Requirement: Integrated API Documentation
All public APIs, data models, protocols, and errors MUST be documented using Swift's standard DocC compatible markup (`///`).

#### Scenario: Using Quick Help
- **WHEN** a developer option-clicks a method in Xcode
- **THEN** they see comprehensive documentation including parameter descriptions, return values, and possible thrown errors

### Requirement: Native Error Mapping
The Swift SDK MUST parse error results from the Go bridge and map them to a strongly-typed, idiomatic Swift `Error` enum.

#### Scenario: Throwing native errors
- **WHEN** a Go SDK operation returns a failure result
- **THEN** the Swift client parses the error JSON and throws a corresponding case from an `EmergentError` enum (e.g., `.networkFailure`, `.unauthorized`)

### Requirement: Modern Swift Concurrency
The Swift SDK MUST expose operations as `async throws` functions instead of requiring the consumer to handle callbacks directly.

#### Scenario: Using async/await
- **WHEN** a Swift application calls an SDK method with `try await`
- **THEN** the SDK uses continuations (`withCheckedThrowingContinuation`) to bridge the C-level callback back to the Swift concurrency task

### Requirement: Continuation Thread Safety
The Swift SDK MUST ensure that continuations are resumed exactly once, handling both success and failure cases returned by the Go bridge safely on arbitrary background threads.

#### Scenario: Safely resuming continuations from background threads
- **WHEN** the Go C-interface executes a callback on a background Go-routine thread
- **THEN** Swift safely resumes the continuation on the appropriate task executor without crashing or leaking memory

### Requirement: Task Cancellation Propagation
The Swift SDK MUST bridge Swift's native Concurrency cancellation (`Task.isCancelled`) to the Go bridge to prevent orphaned background operations.

#### Scenario: Cancelling a Swift Task
- **WHEN** a Swift developer cancels a task executing an SDK operation
- **THEN** the SDK uses `withTaskCancellationHandler` to immediately signal the Go bridge to cancel the associated operation ID

### Requirement: Native Apple Unified Logging
The Swift SDK MUST integrate the Go bridge's log output with Apple's unified logging system (`os.Logger` / OSLog) for native observability.

#### Scenario: Viewing logs in Xcode Console
- **WHEN** the underlying Go SDK emits a log statement
- **THEN** the Swift SDK receives it via the C callback and routes it to `os.Logger`, categorizing it with the correct subsystem and severity level (e.g., debug, error)

### Requirement: Precompiled Distribution
The SDK MUST be distributed as an `XCFramework` via Swift Package Manager (SPM).

#### Scenario: Including the SDK via SPM
- **WHEN** a developer adds the Swift package dependency to their Xcode project
- **THEN** Xcode resolves the dependency and correctly links the precompiled `XCFramework` without requiring a local Go toolchain to build it

### Requirement: Apple Platform Build Hardening
The XCFramework generation MUST adhere to modern Apple platform requirements, explicitly disabling deprecated features like bitcode and ensuring proper code-signing readiness.

#### Scenario: Integrating with modern Xcode versions
- **WHEN** the generated XCFramework is included in an iOS 15+ application
- **THEN** the project builds without bitcode warnings or binary architecture validation errors
