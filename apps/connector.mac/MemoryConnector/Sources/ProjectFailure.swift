import Foundation

/// What a failed project operation means for the connection it was serving.
///
/// The distinction is the whole point of the launch reconcile: a connection is
/// a user decision backed by a long-lived project token and an engine child that
/// is already running, so only a failure that cannot fix itself may tear it
/// down. Treating every failure as permanent is what made a network blip look
/// like a lost project.
enum ProjectFailure: Equatable, Sendable {
    /// The connector CLI has no usable session for the account, the provider
    /// already rejected it, or it expired with no refresh token to renew it.
    /// Keep every connection — its token and config still bind it — and surface
    /// the sign-in affordance.
    case auth
    /// The transport or the server failed in a way that may succeed later: a
    /// timeout, a refused/reset connection, a DNS failure, a 408/429, or any
    /// 5xx. Keep everything and try again later.
    case transient
    /// The project is gone or this account may no longer use it: 403, 404, "no
    /// project matching", and the other 4xx client errors. Reconnecting cannot
    /// fix it; only that project's connection is dropped.
    case permanent

    /// Shared classifier for project operations (`projects use`,
    /// `projects list`).
    ///
    /// The connector CLI's session status is the authority for "auth": a
    /// `needs_reauth` session keeps `signed_in` true, so text alone would miss
    /// it (and `refresh failed with status 400` carries no OAuth code). The
    /// status costs one extra process spawn only on the failure path.
    ///
    /// A session that cannot be renewed is `auth` too: an expired access token
    /// with no refresh token stored (`expired` and not `can_refresh`) would
    /// otherwise be retried as `.transient` forever, when the only fix is the
    /// sign-in affordance.
    ///
    /// `can_refresh` is read as `!= true`, not `== false`: older CLI builds
    /// omit the key for a session with no refresh token (`omitempty` dropped
    /// the false value), and a missing key decodes to `nil`. Absence means the
    /// same thing as false here, and reading it the other way round made this
    /// whole branch unreachable against the CLI that was actually shipped.
    ///
    /// A failure that matches nothing is treated as `transient`. Over-keeping a
    /// connection costs a retry; over-clearing one discards the user's decision
    /// and a running engine, which is the bug this classification exists to
    /// prevent.
    static func classify(_ error: Error,
                         cli: ConnectorCLI,
                         serverURL: String,
                         configPath: String) async -> ProjectFailure
    {
        if AuthFailure.looksLikeAuthFailure(error) {
            return .auth
        }
        // A session the provider has already rejected is still "a session is
        // stored" (`signed_in: true`), so only `needs_reauth` catches it. A
        // session that expired without a refresh token cannot be renewed either:
        // that is a sign-in, not a retry.
        if let status = try? await cli.authStatus(serverURL: serverURL, configPath: configPath) {
            let cannotRenew = status.expired == true && status.canRefresh != true
            if !status.signedIn || status.needsReauth == true || cannotRenew {
                return .auth
            }
        }
        return classifyMessage(textOf(error))
    }

    /// Message-only classification, for the cases the status cannot answer
    /// (the CLI itself is unreachable, so there is no status to read).
    ///
    /// Project commands reach the API through the SDK, whose errors stringify as
    /// `[<status>] <code>: <message>` (e.g. `[404] not_found: project not
    /// found`), so a bracketed status is a reliable signal. The parse lives in
    /// `AuthFailure.statusToken(in:)` so both classifiers accept exactly the
    /// same shapes, and none of them can be fooled by a project name that
    /// contains a status-like number.
    static func classifyMessage(_ text: String) -> ProjectFailure {
        let lowered = text.lowercased()
        if let status = AuthFailure.statusToken(in: lowered) {
            switch status {
            case 401:
                return .auth
            case 403, 404, 410:
                return .permanent
            case 408, 425, 429:
                return .transient
            case 500 ... 599:
                return .transient
            case 400 ... 499:
                // Another client error (bad request, revoked token scope). A
                // retry cannot fix it either.
                return .permanent
            default:
                return .transient
            }
        }
        if transientSignals.contains(where: lowered.contains) {
            return .transient
        }
        if permanentSignals.contains(where: lowered.contains) {
            return .permanent
        }
        return .transient
    }

    /// Transport and server-side signals that a retry can fix.
    private static let transientSignals = [
        "timeout",
        "timed out",
        "i/o timeout",
        "deadline exceeded",
        "connection refused",
        "connection reset",
        "connection aborted",
        "no such host",
        "network is unreachable",
        "network connection was lost",
        "temporary failure",
        "temporarily unavailable",
        "service unavailable",
        "bad gateway",
        "gateway timeout",
        "dial tcp",
        "eof",
        "429",
        "503",
    ]

    /// Signals that the project itself is gone or no longer accessible.
    private static let permanentSignals = [
        "no project matching",
        "not found",
        "forbidden",
        "insufficient permissions",
        "revoked",
        "project is required",
    ]

    /// The human-readable text of `error`, preferring `ConnectorCLIError`'s
    /// description (it preserves the CLI's own message, which carries the SDK
    /// status code).
    private static func textOf(_ error: Error) -> String {
        ((error as? ConnectorCLIError)?.errorDescription ?? error.localizedDescription)
            .lowercased()
    }
}
