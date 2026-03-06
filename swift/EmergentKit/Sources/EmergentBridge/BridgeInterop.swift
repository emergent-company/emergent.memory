/// BridgeInterop.swift — Internal C-interop utilities.
///
/// This module is the **only** place in the Swift SDK that imports the raw
/// `EmergentGoCore` C symbols. All other Swift targets import `EmergentBridge`
/// and use the type-safe wrappers defined here.
///
/// C-symbol encapsulation: everything in this file is `internal` (the default),
/// so no C types leak into the public API of `EmergentKit`.

import EmergentGoCore
import Foundation

// MARK: - String helpers

/// Converts a Swift `String` to a C string, passes it to `body`, and ensures
/// the memory is freed by calling the Go-exported `FreeString` afterward.
@discardableResult
func withBridgeString<T>(
    _ swift: String,
    _ body: (UnsafePointer<CChar>?) -> T
) -> T {
    swift.withCString { ptr in
        body(ptr)
    }
}

/// Converts a C string returned by the Go bridge to a Swift `String` and
/// **frees the C memory** using the exported `FreeString` function.
///
/// - Parameter cStr: A `*C.char` returned by a bridge function. May be nil.
/// - Returns: The Swift string, or an empty string if `cStr` is nil.
func consumeBridgeString(_ cStr: UnsafeMutablePointer<CChar>?) -> String {
    guard let cStr else { return "" }
    defer { FreeString(cStr) }
    return String(cString: cStr)
}

// MARK: - JSON envelope

/// The standard JSON wrapper used by every bridge function.
struct BridgeEnvelope<T: Decodable>: Decodable {
    let result: T?
    let error: String?
}

/// Decodes the standard `{"result":…,"error":"…"}` envelope from a bridge
/// JSON string, throwing an `EmergentError` if the envelope contains an error
/// or if decoding fails.
func decodeBridgeResponse<T: Decodable>(_ json: String) throws -> T {
    guard let data = json.data(using: .utf8) else {
        throw EmergentError.internal("Bridge returned non-UTF8 data")
    }
    let envelope = try JSONDecoder().decode(BridgeEnvelope<T>.self, from: data)
    if let errMsg = envelope.error, !errMsg.isEmpty {
        throw EmergentError.from(message: errMsg)
    }
    guard let result = envelope.result else {
        throw EmergentError.internal("Bridge envelope had no result and no error")
    }
    return result
}

// MARK: - Async callback bridging

/// A box that lets us pass a Swift closure as an opaque `UnsafeMutableRawPointer`
/// through the C callback boundary.
final class CallbackBox<T: Sendable> {
    let continuation: CheckedContinuation<T, Error>
    init(_ continuation: CheckedContinuation<T, Error>) {
        self.continuation = continuation
    }
}

/// Retains a `CallbackBox` and returns a raw pointer suitable for passing to a
/// CGO callback as the `ctx` argument. The caller is responsible for releasing
/// via `Unmanaged.fromOpaque(_:).release()`.
func retainedContext<T: Sendable>(_ box: CallbackBox<T>) -> UnsafeMutableRawPointer {
    Unmanaged.passRetained(box).toOpaque()
}

/// Reclaims a previously retained `CallbackBox` from the opaque `ctx` pointer
/// returned by a CGO callback. Transfers ownership (i.e., releases the retain).
func releaseContext<T: Sendable>(_ ctx: UnsafeMutableRawPointer?) -> CallbackBox<T>? {
    guard let ctx else { return nil }
    return Unmanaged<CallbackBox<T>>.fromOpaque(ctx).takeRetainedValue()
}
