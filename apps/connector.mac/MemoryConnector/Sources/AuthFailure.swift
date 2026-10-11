import Foundation

/// Shared classifier for "the connector CLI has no valid session" failures.
///
/// `ProjectStore` and `AccountStore` both need to tell an expected
/// auth/session failure (which should surface a re-auth affordance, not a raw
/// error) apart from a genuine error. The rules live here so the two stores
/// cannot drift.
enum AuthFailure {
    /// True when `error`'s text carries one of the known not-signed-in /
    /// rejected-token signals. Uses `ConnectorCLIError.errorDescription` when
    /// available (it preserves the CLI's own message) and falls back to the
    /// localized description otherwise.
    static func looksLikeAuthFailure(_ error: Error) -> Bool {
        let text = ((error as? ConnectorCLIError)?.errorDescription ?? error.localizedDescription)
            .lowercased()
        return text.contains("not signed in")
            || text.contains("invalid_token")
            || text.contains("unauthorized")
            || text.contains("401")
    }
}
