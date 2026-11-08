import Foundation

/// Reading a failed chat turn: what to tell the user.
///
/// A pure function over `Error` so ``HTTPChatTransport`` stays a transport and
/// the wording is testable without opening a stream.
enum ChatTurnFailure {
    /// Message for a turn whose stream ended without a single token, when
    /// nothing else explains it.
    ///
    /// The turn is still with Memory: it records the reply when the run ends,
    /// and the view loads it from there (`emergent.memory#445`).
    static let noReply = "The reply did not arrive. Memory records it when the run ends, so it will appear here shortly."

    /// Whether Memory may still record a reply for this failure.
    ///
    /// A stream that ends on its own or drops mid-flight leaves the detached run
    /// going server-side, so the reply exists only in the record. A refused
    /// request, an unreachable gateway, or a body that could not be read means
    /// the run never started (or nothing is coming), so waiting is pointless.
    static func isWorthWaiting(for error: Error?) -> Bool {
        guard let error else { return true }
        guard let gateway = error as? GatewayError else { return true }
        switch gateway {
        case .transport:
            return true
        case .httpStatus, .invalidConfiguration, .decode:
            return false
        }
    }

    /// Message for a failed turn. A misconfigured deployment is reported as
    /// configuration, never as an unreachable server.
    static func message(for error: Error) -> String {
        guard let gateway = error as? GatewayError else {
            return error.localizedDescription
        }
        switch gateway {
        case let .httpStatus(code, message):
            switch code {
            case 401, 403:
                return "This device's credential was rejected. Re-scan the setup code from the web app."
            case 502, 503:
                return "The Memory service is not reachable right now. Your message was not sent."
            default:
                return message ?? "Chat failed with HTTP \(code)."
            }
        case .invalidConfiguration, .transport, .decode:
            return gateway.localizedDescription
        }
    }
}
