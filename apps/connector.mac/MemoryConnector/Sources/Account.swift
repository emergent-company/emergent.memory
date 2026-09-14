import Foundation

/// A signed-in Memory account: one environment + one user identity.
///
/// This is the NON-secret index entry persisted in `accounts.json`. Secrets
/// (the OIDC session and connector tokens) live in the connector CLI's own
/// storage; the app keeps only each account's 0600 directory for profiles,
/// keyed by `id`.
///
/// `id` is stable and derived as `"<environmentID>:<sub-or-email>"` so that
/// signing in again to the same user in the same environment resolves to the
/// same account (and directory) instead of creating a duplicate. Migration of
/// the legacy single session falls back to `"prod:legacy"` when no identity
/// claim is available.
struct Account: Codable, Identifiable, Hashable, Sendable {
    /// `"<environmentID>:<sub-or-email>"` — also the secret directory name.
    let id: String
    /// Which built-in `Environment` this account belongs to (`"prod"`/`"dev"`).
    let environmentID: String
    /// Email from the userinfo/id-token, when the provider returns one.
    var email: String?
    /// Display name from the userinfo/id-token, when available.
    var displayName: String?
    /// Optional Memory avatar object key (filled in by a later API refresh).
    var avatarObjectKey: String?

    /// The resolved built-in environment, when the id is known.
    var environment: Environment? {
        Environment.environment(id: environmentID)
    }

    /// Compact environment name shown next to the account ("Prod" / "Dev"),
    /// falling back to the persisted id's capitalized form for unknown ids.
    var environmentLabel: String {
        environment?.shortLabel ?? environmentID.capitalized
    }

    /// Primary switcher-row label: the email address, so two accounts are
    /// never confused. Falls back to the environment label only when the index
    /// has no email — never the initials or a generic "Signed in".
    var switcherTitle: String {
        if let email = Self.trimmed(email) { return email }
        return environmentLabel
    }

    /// Primary label used outside the switcher (sign-out prompts): the display
    /// name, else the email, else the environment label. Never returns a
    /// generic "Signed in" fallback.
    var displayTitle: String {
        if let name = Self.trimmed(displayName) { return name }
        if let email = Self.trimmed(email) { return email }
        return environmentLabel
    }

    private static func trimmed(_ value: String?) -> String? {
        guard let value else { return nil }
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }

    /// Secondary row label: the email when a distinct display name is shown.
    var subtitle: String? {
        guard let name = displayName?.trimmingCharacters(in: .whitespacesAndNewlines),
              !name.isEmpty,
              let email = email?.trimmingCharacters(in: .whitespacesAndNewlines),
              !email.isEmpty else {
            return nil
        }
        return email
    }

    /// Initials for the avatar glyph (never used as a display name).
    var initials: String {
        MemoryIdentity.initials(from: displayName ?? email ?? "")
    }
}
