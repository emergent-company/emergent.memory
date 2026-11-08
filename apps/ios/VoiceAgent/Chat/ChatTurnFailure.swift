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

    /// Message for a reply the stream stopped delivering before the turn ended —
    /// no error, no `done`, just no more frames.
    ///
    /// The same wait recovers it: what is on screen is a prefix of the reply
    /// Memory is recording.
    static let cutShort = "The reply stopped before it finished. Memory records it when the run ends, so it will appear here shortly."

    /// Whether Memory may still record a reply for this turn end.
    ///
    /// A reply can only exist if the gateway accepted the turn, and one decoded
    /// frame proves it did. Without a frame, only a stream that *ended* can be an
    /// accepted turn whose events were all empty — the reported
    /// `emergent.memory#445` case. A connection that never opened, a refused
    /// request or a body that could not be read leaves nothing to wait for: the
    /// banner is the whole answer, and polling would spend the window on a reply
    /// that is not coming. A mid-flight drop has necessarily seen a frame.
    static func isWorthWaiting(for error: Error?, sawFrame: Bool) -> Bool {
        sawFrame || error == nil
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
