import Foundation
import LiveKit

/// User-facing classification of a failed connection.
/// Plain enum + `LocalizedError` so the failure UI stays trivially restyleable.
enum MemoryFailure: LocalizedError, Equatable {
    /// The deployment has no voice stack configured, so the token endpoint
    /// answers `503` instead of minting a token. Not a transient failure:
    /// retrying asks the same deployment the same question.
    case voiceNotConfigured
    /// The token endpoint could not be reached, returned a non-2xx status,
    /// or returned an undecodable body.
    case tokenFetch(message: String)
    /// The token was obtained but the LiveKit server did not accept the
    /// connection (unreachable host, bad token, rejected room, ...).
    case serverUnreachable(message: String)
    /// The room connected but the agent never joined within
    /// `MemoryConfig.agentConnectTimeout`.
    case agentNotJoined(agentName: String)
    /// The microphone permission was denied (LiveKit `deviceAccessDenied`).
    case micPermissionDenied(agentName: String)
    /// Any other failure.
    case unknown(message: String)

    var errorDescription: String? {
        switch self {
        case .voiceNotConfigured:
            "Voice isn't available on this deployment. Text chat works without it."
        case let .tokenFetch(message):
            "Could not get a token from the Memory token server.\n\(message)"
        case let .serverUnreachable(message):
            "Could not connect to the LiveKit server.\n\(message)"
        case let .agentNotJoined(agentName):
            "\(agentName) did not join the room in time. The agent worker may be unavailable."
        case let .micPermissionDenied(agentName):
            "Microphone access is required to talk to \(agentName). Enable it in Settings > Privacy > Microphone."
        case let .unknown(message):
            message
        }
    }

    /// Whether repeating the action could succeed.
    ///
    /// A deployment without a voice stack will not acquire one between two
    /// taps, so the failure view offers no retry for it — the button would only
    /// teach the user that the app is broken. Everything else here is
    /// plausibly transient and keeps its retry.
    var isRetryable: Bool {
        switch self {
        case .voiceNotConfigured:
            false
        case .tokenFetch, .serverUnreachable, .agentNotJoined, .micPermissionDenied, .unknown:
            true
        }
    }
}
