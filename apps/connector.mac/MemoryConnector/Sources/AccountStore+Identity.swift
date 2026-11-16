import Foundation

/// Background backfill of a restored account's display identity.
///
/// The non-secret index (`accounts.json`) can outlive the identity captured at
/// sign-in, so the app re-fetches email/name/avatar for an account that is
/// missing them. Best-effort: a failure never throws and never blocks the UI.
extension AccountStore {
    // MARK: - Identity backfill

    /// Fills in a restored account's `email`/`displayName`/avatar from the
    /// Memory API when the index lacks identity. Uses THAT account's access
    /// token and environment server URL, then persists the enriched entry into
    /// the index.
    ///
    /// Best-effort and idempotent: a no-op when `email` and `displayName` are
    /// already present, and any fetch/token failure leaves the account
    /// untouched. Never throws and never blocks the UI.
    func refreshIdentity(for accountID: String) async {
        guard let account = account(id: accountID) else { return }
        guard Self.needsIdentityBackfill(account) else { return }
        guard let environment = account.environment else { return }
        guard let token = try? await currentAccessToken(for: accountID) else { return }

        let client = memoryClientFactory(environment.serverURLString, token)
        var email = account.email?.nilIfBlank
        var displayName = account.displayName?.nilIfBlank
        var avatar = account.avatarObjectKey?.nilIfBlank

        if let profile = try? await client.userProfile() {
            email = profile.email?.nilIfBlank ?? email
            displayName = profile.displayName?.nilIfBlank
                ?? Self.combinedName(profile)
                ?? displayName
            avatar = profile.avatarObjectKey?.nilIfBlank ?? avatar
        }
        // `/api/user/profile` is the richest source; `/api/auth/me` is the
        // fallback for the email when the profile is unavailable.
        if email == nil, let authUser = try? await client.authMe() {
            email = authUser.email?.nilIfBlank ?? email
        }

        let enriched = Account(id: account.id,
                               environmentID: account.environmentID,
                               email: email,
                               displayName: displayName,
                               avatarObjectKey: avatar)
        guard enriched != account else { return }
        upsert(enriched)
        persistIndex()
    }

    /// Backfills the active account, when one is signed in. Launch/restore hook.
    func backfillIdentityIfNeeded() async {
        guard let id = activeAccountID else { return }
        await refreshIdentity(for: id)
    }

    /// Only fetch when identity is genuinely missing. `avatarObjectKey` is
    /// filled opportunistically during a backfill but is not itself a trigger:
    /// users may have no avatar, and re-fetching on every launch would never
    /// settle. An account with an email or a display name needs no backfill.
    private nonisolated static func needsIdentityBackfill(_ account: Account) -> Bool {
        account.email?.nilIfBlank == nil || account.displayName?.nilIfBlank == nil
    }

    private nonisolated static func combinedName(_ profile: UserProfile) -> String? {
        let parts = [profile.firstName?.nilIfBlank, profile.lastName?.nilIfBlank].compactMap(\.self)
        return parts.isEmpty ? nil : parts.joined(separator: " ")
    }
}
