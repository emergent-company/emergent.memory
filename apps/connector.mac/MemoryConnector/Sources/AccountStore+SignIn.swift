import Foundation

/// The account lifecycle: interactive sign-in, account switch, and sign-out.
///
/// All three run through the bundled `memory-connector` CLI, which owns the
/// OIDC session; the app keeps the non-secret index and per-account directories
/// in sync. Session reconciliation itself lives in `AccountStore+Session.swift`.
extension AccountStore {
    // MARK: - Sign in

    /// Runs the connector CLI two-step PKCE flow against `environment`, upserts
    /// the account in the index, and makes it active. Returns the (new or
    /// existing) account.
    ///
    /// The CLI (`auth start` / `auth complete`) owns PKCE, the token exchange,
    /// and session storage; the app only opens the returned `authorize_url` and
    /// relays the callback's `code`/`state`. The browser round trip itself is
    /// `completeCLISignIn`.
    @discardableResult
    func signIn(environment: Environment) async throws -> Account {
        lastError = nil
        isSigningIn = true
        defer { isSigningIn = false }
        do {
            let email = try await completeCLISignIn(environment: environment)
            let accountID = Self.accountID(environment: environment, email: email)
            let account = Account(id: accountID,
                                  environmentID: environment.id,
                                  email: email,
                                  displayName: nil,
                                  avatarObjectKey: nil)
            upsert(account)
            persistIndex()
            activate(account, source: "signIn")
            // The CLI just completed sign-in, so the session is valid now.
            markSessionValid(for: accountID)
            // The CLI status carries only the email; fill in a display name and
            // avatar from the API in the background (idempotent; a no-op when
            // the account already carries identity).
            Task { await refreshIdentity(for: accountID) }
            return account
        } catch {
            let mapped = Self.oidcError(from: error)
            lastError = mapped.localizedDescription
            throw mapped
        }
    }

    /// Runs the CLI's two-step PKCE flow: `auth start`, the browser round trip,
    /// then `auth complete`. Returns the email the CLI reports for the session.
    private func completeCLISignIn(environment: Environment) async throws -> String? {
        let start = try await connectorCLI.authStart(serverURL: environment.serverURLString,
                                                     redirectURI: OAuthCallback.redirectURI,
                                                     clientID: environment.clientID,
                                                     issuer: nil,
                                                     configPath: configPath)
        guard let authorizeURL = URL(string: start.authorizeURL) else {
            throw OIDCError.invalidResponse("the connector returned an invalid authorization URL")
        }

        let callback = try await authenticator.authenticate(url: authorizeURL,
                                                            callbackURLScheme: OAuthCallback.callbackScheme)
        let items = OAuthCallback.queryItems(from: callback)
        if let error = items["error"] {
            throw OIDCError.invalidResponse(items["error_description"] ?? error)
        }
        guard items["state"] == start.state else { throw OIDCError.stateMismatch }
        guard let code = items["code"], !code.isEmpty else {
            throw OIDCError.invalidResponse("authorization callback is missing the code")
        }

        let status = try await connectorCLI.authComplete(serverURL: environment.serverURLString,
                                                         loginID: start.loginID,
                                                         code: code,
                                                         state: start.state,
                                                         configPath: configPath)
        guard status.signedIn else {
            throw OIDCError.invalidResponse("the connector did not complete sign-in")
        }
        return status.email
    }

    // MARK: - Switching

    /// Disconnect the previous account's engine, persist the new active
    /// account, and make it active. The account must be signed in per the CLI
    /// (`auth status`); other accounts' data is untouched.
    func switchTo(accountID: String) async throws {
        guard let account = account(id: accountID) else {
            throw AccountStoreError.unknownAccount(accountID)
        }
        guard activeAccountID != accountID else { return }
        guard let environment = account.environment else {
            throw AccountStoreError.unknownAccount(accountID)
        }
        do {
            let status = try await connectorCLI.authStatus(serverURL: environment.serverURLString,
                                                           configPath: configPath)
            guard status.signedIn else { throw OIDCError.notSignedIn }
            // `signed_in` only says a session is stored. One the provider has
            // already rejected must not switch in as valid — it needs a new
            // sign-in, and the account keeps that mark so the UI can offer it.
            if status.needsReauth == true {
                markNeedsReauth(accountID)
                throw OIDCError.notSignedIn
            }
        } catch {
            throw Self.oidcError(from: error)
        }
        activate(account, source: "switchTo")
        // The CLI validated this account's session above.
        markSessionValid(for: accountID)
        // Restore/activate: enrich a stale index entry in the background.
        Task { await refreshIdentity(for: accountID) }
    }

    // MARK: - Sign out

    /// Removes one account: best-effort CLI logout, delete its secret directory
    /// and index entry, and promote another account when the removed one was
    /// active. Other accounts are left completely untouched.
    func signOut(accountID: String) async {
        guard let account = account(id: accountID) else { return }
        let wasActive = activeAccountID == accountID
        if wasActive {
            engineWillStop()
        }

        // Best-effort: drop the CLI-side session for this environment. Errors
        // (already signed out, CLI unavailable) are ignored.
        if let environment = account.environment {
            try? await connectorCLI.authLogout(serverURL: environment.serverURLString, configPath: configPath)
        }

        identities[accountID] = nil
        clearNeedsReauth(accountID)
        silentReauthTasks[accountID]?.cancel()
        silentReauthTasks[accountID] = nil
        sessionValid[accountID] = nil
        tokenCache[accountID] = nil
        tokenTasks[accountID]?.cancel()
        tokenTasks[accountID] = nil
        // A stale failed-sign-in message must not linger on the Connection /
        // About surfaces after this account is removed.
        lastError = nil
        try? FileManager.default.removeItem(at: accountDirectory(for: accountID))

        accounts.removeAll { $0.id == accountID }
        persistIndex()

        if wasActive {
            let next = accounts.first
            activeAccountID = next?.id
            if let next {
                defaults.set(next.id, forKey: Self.activeAccountIDKey)
            } else {
                defaults.removeObject(forKey: Self.activeAccountIDKey)
            }
            ConnectorLog.lifecycle("activeAccountDidChange source=signOut account=\(next?.id ?? "nil")")
            activeAccountDidChange(next)
            // Re-derive the effective state for the promoted account (or clear
            // it when the last account is gone).
            await reconcileEffectiveSignedInState()
        }
    }

    // MARK: - Activation

    /// Makes `account` active: stops the previous account's engine, persists the
    /// choice, and notifies the app graph. Shared by sign-in and switch.
    private func activate(_ account: Account, source: String) {
        if let previous = activeAccountID, previous != account.id {
            engineWillStop()
        }
        guard activeAccountID != account.id else { return }
        activeAccountID = account.id
        defaults.set(account.id, forKey: Self.activeAccountIDKey)
        ConnectorLog.lifecycle("activeAccountDidChange source=\(source) account=\(account.id)")
        activeAccountDidChange(account)
    }

    /// Post-activation bookkeeping once the CLI has confirmed a usable session.
    /// Clearing the re-auth mark also ends its silent-attempt episode.
    private func markSessionValid(for accountID: String) {
        clearNeedsReauth(accountID)
        sessionValid[accountID] = true
        tokenCache[accountID] = nil
        setSessionState(.valid)
    }
}
