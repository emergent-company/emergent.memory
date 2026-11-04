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
