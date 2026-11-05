import Foundation
import LiveKit

extension MemorySessionController {
    // MARK: - Failure classification

    func classify(_ error: Session.Error) -> MemoryFailure {
        guard case let .connection(underlying) = error else {
            return .unknown(message: error.localizedDescription)
        }
        return classify(underlying)
    }

    func classify(_ error: any Error) -> MemoryFailure {
        if let tokenError = error as? GatewayError {
            // `503` from the token endpoint is the gateway saying it has no
            // LiveKit credentials to mint with (`web-ui/gateway/token.go`), not
            // a transient outage: text chat is unaffected and retrying the call
            // answers the same. Anything else is a real token-fetch failure.
            if case let .httpStatus(code, _) = tokenError, code == 503 {
                return .voiceNotConfigured
            }
            return .tokenFetch(message: tokenError.localizedDescription)
        }
        if let liveKitError = error as? LiveKitError {
            switch liveKitError.type {
            case .deviceAccessDenied:
                return .micPermissionDenied(agentName: agentDisplayName)
            case .network:
                return .serverUnreachable(message: error.localizedDescription)
            default:
                break
            }
        }
        return .serverUnreachable(message: error.localizedDescription)
    }

    func classify(agentError: LiveKit.Agent.Error) -> MemoryFailure {
        switch agentError {
        case .timeout:
            .agentNotJoined(agentName: agentDisplayName)
        case .left:
            .unknown(message: agentError.localizedDescription)
        }
    }
}
