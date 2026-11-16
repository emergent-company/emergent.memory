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
    ///
    /// `invalid_grant` is the provider's own code for exactly this condition
    /// (the refresh token expired, was revoked, or was rotated away); a refresh
    /// failure that carries it must not be treated as transient, or the account
    /// silently retries a token that can never work again.
    ///
    /// The HTTP status is read through `statusToken(in:)`, never by looking for
    /// the digits alone: a project name, path or id containing `401` (e.g.
    /// `[Work 401 Audit] create failed`) is not an auth failure.
    static func looksLikeAuthFailure(_ error: Error) -> Bool {
        let text = ((error as? ConnectorCLIError)?.errorDescription ?? error.localizedDescription)
            .lowercased()
        return text.contains("not signed in")
            || text.contains("invalid_grant")
            || text.contains("invalid_token")
            || text.contains("unauthorized")
            || statusToken(in: text) == 401
    }

    /// The first HTTP status token in `text`, when it carries one.
    ///
    /// Only the shapes the connector CLI and the server SDK actually emit are
    /// accepted, and each is anchored to its own syntax so text that merely
    /// contains the digits (a project name, a port, an id) can never be read as
    /// a status:
    ///
    /// - `[<status>] …` / `[<status>] <code>: …` — the server SDK's error text
    ///   (`apps/server/pkg/sdk/errors`: `fmt.Sprintf("[%d] %s", …)`, the detail
    ///   the CLI prefixes with `memory-connector projects use: …`). The closing
    ///   bracket is required, so a bracketed name that starts with three digits
    ///   (`[404 Reasons] …`) does not match and the scan continues.
    /// - `status <status>` — the CLI/Go shape for a refresh failure, e.g.
    ///   `refresh failed with status 400`.
    /// - `HTTP <status>` — `OIDCError.httpStatus`'s own text, e.g.
    ///   `The identity provider returned HTTP 401.`
    ///
    /// Digits alone are never a token: `[Work 401 Audit] create failed` has no
    /// status, and a bare `404` that the CLI does not emit as `[404]` is left
    /// unparsed rather than guessed at.
    static func statusToken(in text: String) -> Int? {
        guard let regex = statusRegex else { return nil }
        let full = NSRange(text.startIndex ..< text.endIndex, in: text)
        guard let match = regex.firstMatch(in: text, options: [], range: full) else { return nil }
        for group in 1 ... 3 {
            let range = match.range(at: group)
            guard range.location != NSNotFound, let value = Int((text as NSString).substring(with: range))
            else { continue }
            return value
        }
        return nil
    }

    /// The three anchored shapes above, sharing one left-to-right scan so the
    /// first recognised token wins (the status that follows a bracketed project
    /// name cannot be shadowed by the name).
    private static let statusRegex = try? NSRegularExpression(
        pattern: #"\[(\d{3})\]|\bstatus\s+(\d{3})\b|\bhttp\s+(\d{3})\b"#,
        options: [.caseInsensitive]
    )
}
