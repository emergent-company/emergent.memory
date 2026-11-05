import Foundation

/// Reading a failed chat turn: what to tell the user.
///
/// A pure function over `Error` so ``HTTPChatTransport`` stays a transport and
/// the wording is testable without opening a stream.
enum ChatTurnFailure {
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
