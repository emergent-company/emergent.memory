## ADDED Requirements

### Requirement: Go SDK C-Interface Exports
The Go SDK MUST provide C-compatible exported functions (`//export`) for all necessary client operations. These functions MUST take and return C-compatible primitives (e.g., `*C.char` for JSON strings) to pass complex data across the CGO boundary.

#### Scenario: Calling a Go function from C
- **WHEN** a C-compatible function like `CreateClient` is called with valid configuration parameters as a JSON string
- **THEN** it returns a C-compatible string containing the serialized result or error

### Requirement: Cross-Boundary Memory Management
The Go SDK MUST provide explicit memory deallocation functions for any heap-allocated C strings returned to the caller.

#### Scenario: Freeing returned strings
- **WHEN** the Swift/C client finishes using a string returned from the Go bridge
- **THEN** it calls the corresponding exported `FreeString` function to prevent memory leaks

### Requirement: Standardized Error Boundary
The Go SDK MUST return errors via a standardized JSON wrapper (e.g., `{"result": ..., "error": "..."}`) or explicit C-structs to ensure deterministic error mapping in Swift.

#### Scenario: Returning an error
- **WHEN** a Go SDK operation fails internally
- **THEN** it returns a JSON payload containing the `error` string, allowing the Swift client to throw a native `Error`

### Requirement: Asynchronous Operation Callbacks
The Go SDK C-interface MUST support asynchronous operations by accepting C function pointers as callbacks.

#### Scenario: Executing async tasks
- **WHEN** a C function for an asynchronous task is invoked with a callback function pointer
- **THEN** the Go bridge executes the task in a goroutine and invokes the callback with the result upon completion

### Requirement: Safe Context Passing for Callbacks
The Go C-interface MUST accept an opaque context pointer (e.g., `unsafe.Pointer`) for asynchronous operations to identify the caller context, and pass it back unmodified in the callback.

#### Scenario: Preserving Swift state across CGO
- **WHEN** an async task is initiated with a Swift context pointer
- **THEN** the Go bridge returns the exact same pointer to the callback, enabling Swift to resume the correct continuation

### Requirement: Task Cancellation Support
The Go C-interface MUST provide a mechanism to cancel long-running asynchronous operations, mapping a unique operation identifier to the underlying Go `context.Context` cancellation function.

#### Scenario: Cancelling an active Go routine
- **WHEN** the Swift client requests cancellation for a specific operation ID
- **THEN** the Go bridge invokes the corresponding `context.CancelFunc`, gracefully terminating the underlying Go SDK operation and returning a cancellation error via the callback

### Requirement: Log Bridging
The Go SDK MUST support routing its internal log output through a C callback function, rather than relying exclusively on stdout/stderr.

#### Scenario: Intercepting internal logs
- **WHEN** a Go SDK component logs a message
- **THEN** the C-bridge forwards the log level and message string to a registered C callback, allowing the host application to handle it
