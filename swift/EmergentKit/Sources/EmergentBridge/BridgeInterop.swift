/// BridgeInterop.swift — C-interop utilities for the Go bridge.
///
/// This module owns the C-interop helpers (string ownership, callback boxes)
/// and is the only target that *re-exports* nothing: `EmergentGoCore` is a
/// dependency of both `EmergentBridge` and `EmergentKit`, so the C symbols are
/// reachable from the client implementation without any of them escaping to
/// consumers — neither target is a product, and `import EmergentKit` exposes
/// only `EmergentKit`'s own declared API.
///
/// (These members must be `public`: Swift access control is per-module, and
/// `internal` symbols in this target are invisible to `EmergentKit`.)

import EmergentGoCore
import Foundation

// MARK: - String helpers

/// Converts a Swift `String` to a C string, passes it to `body`, and ensures
/// the memory is freed by calling the Go-exported `FreeString` afterward.
@discardableResult
public func withBridgeString<T>(
    _ swift: String,
    _ body: (UnsafeMutablePointer<CChar>?) -> T
) -> T {
    // cgo maps `char *` arguments to `UnsafeMutablePointer<CChar>`, but Go only
    // reads the config/request JSON, never writes it — so handing it the
    // mutable view of the immutable C string is safe and keeps call sites
    // free of casts.
    swift.withCString { ptr in
        body(UnsafeMutablePointer(mutating: ptr))
    }
}

/// Converts a C string returned by the Go bridge to a Swift `String` and
/// **frees the C memory** using the exported `FreeString` function.
///
/// - Parameter cStr: A `*C.char` returned by a bridge function. May be nil.
/// - Returns: The Swift string, or an empty string if `cStr` is nil.
public func consumeBridgeString(_ cStr: UnsafeMutablePointer<CChar>?) -> String {
    guard let cStr else { return "" }
    defer { FreeString(cStr) }
    return String(cString: cStr)
}

// MARK: - Async callback bridging

/// A box that lets us pass a Swift closure as an opaque `UnsafeMutableRawPointer`
/// through the C callback boundary.
public final class CallbackBox<T: Sendable> {
    public let continuation: CheckedContinuation<T, Error>
    public init(_ continuation: CheckedContinuation<T, Error>) {
        self.continuation = continuation
    }
}

/// Retains a `CallbackBox` and returns a raw pointer suitable for passing to a
/// CGO callback as the `ctx` argument. The caller is responsible for releasing
/// via `Unmanaged.fromOpaque(_:).release()`.
public func retainedContext<T: Sendable>(_ box: CallbackBox<T>) -> UnsafeMutableRawPointer {
    Unmanaged.passRetained(box).toOpaque()
}

/// Reclaims a previously retained `CallbackBox` from the opaque `ctx` pointer
/// returned by a CGO callback. Transfers ownership (i.e., releases the retain).
public func releaseContext<T: Sendable>(_ ctx: UnsafeMutableRawPointer?) -> CallbackBox<T>? {
    guard let ctx else { return nil }
    return Unmanaged<CallbackBox<T>>.fromOpaque(ctx).takeRetainedValue()
}
