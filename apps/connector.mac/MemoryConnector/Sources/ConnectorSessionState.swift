import Foundation

/// The connector CLI session state for the active account.
///
/// This used to be a single stored Bool (`isEffectivelySignedIn`), so ANY
/// failure while reading a token — an unreachable CLI, a killed process, a
/// network blip during a refresh — flipped the UI to "signed out", and the
/// natural next click threw away a session that was still perfectly renewable.
/// The states below separate what only the user can fix (`needsReauth`) from
/// what a retry fixes (`offline`).
enum ConnectorSessionState: String, Equatable, Sendable {
    /// Not checked yet, or no account is active.
    case unknown
    /// A stored session the CLI reports usable. Its access token may be
    /// expired; the CLI renews it on demand (`auth access-token`).
    case valid
    /// A session check or renewal is in flight. The signed-in chrome is kept so
    /// a refresh never flashes a sign-in screen.
    case refreshing
    /// The CLI could not be reached, or answered with a transient failure. The
    /// stored session is untouched; the next check decides.
    case offline
    /// The provider rejected the stored refresh token (expired, revoked, or
    /// rotated away from this machine). Only a new sign-in — silent first, then
    /// interactive — can recover this account.
    case needsReauth

    /// True when the UI should keep the account signed in. `offline` is
    /// deliberately included: a machine that cannot reach the CLI right now is
    /// not a machine the user signed out of.
    var keepsSignedIn: Bool {
        switch self {
        case .valid, .refreshing, .offline:
            true
        case .unknown, .needsReauth:
            false
        }
    }
}
