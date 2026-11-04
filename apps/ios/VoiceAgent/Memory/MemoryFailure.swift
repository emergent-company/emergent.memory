import Foundation
import LiveKit

/// User-facing classification of a failed connection.
/// Plain enum + `LocalizedError` so the failure UI stays trivially restyleable.
enum MemoryFailure: LocalizedError, Equatable {
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
}
