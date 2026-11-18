import Foundation

/// Access-token reads, the short-lived cache, and single-flight coalescing.
///
/// Every token read spawns a `memory-connector` process, and one launch reads a
/// token several times (dashboard, engine config, project list, org names), so a
/// short cache removes a burst of process spawns; concurrent callers for one
/// account share a single CLI invocation. Cache state is declared in
/// `AccountStore.swift`.
extension AccountStore {
    // MARK: - Access token cache

    /// A cached token is reused only while it keeps at least this much life: a
    /// token that expires inside the floor is renewed rather than handed to an
    /// API call that would fail moments later.
    nonisolated static let tokenCacheFloor: TimeInterval = 60

    /// True while `token` keeps at least `tokenCacheFloor` of life.
    private nonisolated static func tokenIsUsable(_ token: CachedToken, now: Date = Date()) -> Bool {
        token.expiresAt.timeIntervalSince(now) > tokenCacheFloor
    }

    /// Parses the CLI's RFC 3339 `expires_at` (with or without fractional
    /// seconds). A value that does not parse yields nil, and the token is then
    /// simply not cached.
    nonisolated static func parseExpiry(_ value: String) -> Date? {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = formatter.date(from: trimmed) {
            return date
        }
        formatter.formatOptions = [.withInternetDateTime]
        return formatter.date(from: trimmed)
    }

    // MARK: - Access tokens

    /// A usable (refreshed if needed) access token for the active account.
    func currentAccessToken() async throws -> String {
        guard let activeAccountID else { throw OIDCError.notSignedIn }
        return try await currentAccessToken(for: activeAccountID)
    }

    /// A usable (refreshed if needed) access token for one account, obtained
    /// from the connector CLI (`auth access-token`, which renews a token near
    /// expiry — and serializes that renewal across processes).
    ///
    /// Two things keep a burst of reads cheap: a short-lived cache of the last
    /// token (each read is a process spawn) and single-flight coalescing, so
    /// concurrent callers for one account share a single CLI invocation.
    ///
    /// Failure classification is the point of `ConnectorSessionState`: only an
    /// auth rejection marks the account as needing re-auth (and is mapped to
    /// `OIDCError.notSignedIn` so callers can offer a sign-in affordance). A
    /// transient failure — CLI unreachable, timeout, sandbox denial — surfaces
    /// the error but leaves the account signed in, because signing the user out
    /// for a network blip is what made the connector feel like it forgot them.
    func currentAccessToken(for accountID: String) async throws -> String {
        if let cached = tokenCache[accountID], Self.tokenIsUsable(cached) {
            return cached.accessToken
        }
        if let inFlight = tokenTasks[accountID] {
            return try await inFlight.value
        }
        let task = Task { try await self.fetchAccessToken(for: accountID) }
        tokenTasks[accountID] = task
        defer { tokenTasks[accountID] = nil }
        return try await task.value
    }

    /// The uncached token read. Callers must go through
    /// `currentAccessToken(for:)` so the single-flight coalescing applies.
    ///
    /// - Parameter allowSilentReauth: when false, an auth rejection marks the
    ///   account instead of starting another `prompt=none` attempt. The read
    ///   retried after a successful silent attempt passes false, so a provider
    ///   that rejects again cannot loop the browser.
    private func fetchAccessToken(for accountID: String, allowSilentReauth: Bool = true) async throws -> String {
        guard let environment = account(id: accountID)?.environment else {
            markNeedsReauth(accountID)
            throw AccountStoreError.unknownAccount(accountID)
        }
        do {
            let token = try await connectorCLI.authAccessToken(serverURL: environment.serverURLString,
                                                               configPath: configPath)
            clearNeedsReauth(accountID)
            sessionValid[accountID] = true
            if let expiresAt = Self.parseExpiry(token.expiresAt) {
                tokenCache[accountID] = CachedToken(accessToken: token.accessToken, expiresAt: expiresAt)
            }
            if accountID == activeAccountID {
                setSessionState(.valid)
            }
            return token.accessToken
        } catch {
            tokenCache[accountID] = nil
            if await isAuthRejection(error: error, environment: environment) {
                // A rejected session is the one recoverable case a background
                // flow may fix without asking: try `prompt=none` once, then read
                // the (now renewed) token. The retried read cannot re-attempt.
                if allowSilentReauth, await attemptSilentReauth(for: accountID) {
                    return try await fetchAccessToken(for: accountID, allowSilentReauth: false)
                }
                markNeedsReauth(accountID)
                throw OIDCError.notSignedIn
            }
            if accountID == activeAccountID {
                setSessionState(.offline)
            }
            throw Self.oidcError(from: error)
        }
    }
}
