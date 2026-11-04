import Foundation

/// Reading a failed chat turn: what to tell the user, and whether the run may
/// still have written a reply worth reloading.
///
/// Pure functions over `Error` so ``HTTPChatTransport`` stays a transport and
/// both rules are testable without opening a stream.
enum ChatTurnFailure {
    /// True when the failure happened after the gateway accepted the request,
    /// i.e. the stream died mid-turn rather than the turn being refused.
    ///
    /// `httpStatus` and `invalidConfiguration` both fail before anything runs, so
    /// there is no record to reload; a connection that drops while frames are
    /// arriving (or a decode failure on one) leaves a run that may still write.
    static func droppedMidTurn(_ error: Error) -> Bool {
        guard let gateway = error as? GatewayError else { return true }
        switch gateway {
        case .httpStatus, .invalidConfiguration:
            return false
        case .transport, .decode:
            return true
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
