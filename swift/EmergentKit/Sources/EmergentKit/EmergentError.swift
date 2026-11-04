import Foundation

/// Strongly-typed errors thrown by the `EmergentClient`.
///
/// Bridge error messages from the Go side are parsed by ``EmergentError/from(message:)``
/// and mapped to the appropriate case.  Any unmapped message falls through to
/// ``EmergentError/unknown(_:)``.
public enum EmergentError: Error, Sendable, LocalizedError {

    // MARK: - Cases

    /// The request was not authorised (401 / 403 from the server).
    case unauthorized(String)

    /// A network-level failure (timeout, DNS resolution failure, etc.).
    case networkFailure(String)

    /// The request payload was invalid (client-side validation or 400 from server).
    case invalidRequest(String)

    /// The requested resource could not be found (404).
    case notFound(String)

    /// The operation was cancelled by the caller.
    case cancelled

    /// The server returned an unexpected 5xx response.
    case serverError(String)

    /// An error originating within the Swift SDK itself (not from the server).
    case `internal`(String)

    /// A bridge error that could not be mapped to a more specific case.
    case unknown(String)

    // MARK: - LocalizedError

    public var errorDescription: String? {
        switch self {
        case .unauthorized(let msg):    return "Unauthorized: \(msg)"
        case .networkFailure(let msg):  return "Network failure: \(msg)"
        case .invalidRequest(let msg):  return "Invalid request: \(msg)"
        case .notFound(let msg):        return "Not found: \(msg)"
        case .cancelled:                return "The operation was cancelled."
        case .serverError(let msg):     return "Server error: \(msg)"
        case .internal(let msg):        return "Internal SDK error: \(msg)"
        case .unknown(let msg):         return "Error: \(msg)"
        }
    }

    // MARK: - Factory

    /// Maps a free-form error message from the Go bridge to a typed Swift error.
    static func from(message: String) -> EmergentError {
        let lower = message.lowercased()
        if lower.contains("unauthorized") || lower.contains("forbidden") || lower.contains("401") || lower.contains("403") {
            return .unauthorized(message)
        }
        if lower.contains("not found") || lower.contains("404") {
            return .notFound(message)
        }
        if lower.contains("context canceled") || lower.contains("context cancelled") || lower.contains("canceled") {
            return .cancelled
        }
        if lower.contains("timeout") || lower.contains("connection refused") || lower.contains("no such host") || lower.contains("network") {
            return .networkFailure(message)
        }
        if lower.contains("invalid") || lower.contains("bad request") || lower.contains("400") {
            return .invalidRequest(message)
        }
        if lower.contains("internal server error") || lower.contains("500") || lower.contains("502") || lower.contains("503") {
            return .serverError(message)
        }
        return .unknown(message)
    }
}
