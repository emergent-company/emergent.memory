import Foundation

/// Non-interactive (`prompt=none`) re-auth for an account whose stored session
/// the provider rejected.
///
/// It runs the SAME `auth start`/`auth complete` pair the interactive flow uses,
/// but a decline leaves the account exactly as it was: the `needs_reauth` mark
/// and the interactive affordance stay, and `lastError` is never written. One
/// attempt is allowed per episode — see `needsReauth` / `clearNeedsReauth`.
extension AccountStore {
    /// Non-interactive re-auth for an account whose stored session the provider
    /// rejected (`needs_reauth`). It runs the SAME `auth start`/`auth complete`
    /// pair the interactive flow uses, with `prompt=none` and the account's email
    /// as `login_hint`; a decline ("login_required", a cancelled session, a
    /// browser timeout) cancels the pending login and changes nothing: the mark
    /// and the interactive affordance stay, and `lastError` is never written.
    ///
    /// - Returns: true when the attempt restored a usable session.
    @discardableResult
    func attemptSilentReauth(for accountID: String) async -> Bool {
        guard let account = account(id: accountID), let environment = account.environment else {
            return false
        }
        // Join an in-flight attempt first: whoever started it must see its
        // result, not a second `false`.
        if let inFlight = silentReauthTasks[accountID] {
            return await inFlight.value
        }
        // One attempt per `needs_reauth` episode: the guard is dropped with the
        // mark (`clearNeedsReauth`), so a later episode gets a fresh attempt.
        guard !(needsReauth.contains(accountID) && silentReauthAttempted.contains(accountID)) else {
            return false
        }
        silentReauthAttempted.insert(accountID)
        let task = Task { await self.runSilentReauth(account: account, environment: environment) }
        silentReauthTasks[accountID] = task
        let recovered = await task.value
        silentReauthTasks[accountID] = nil
        if recovered {
            tokenCache[accountID] = nil
        }
        return recovered
    }

    /// The body of `attemptSilentReauth`: start → authorize → complete.
    private func runSilentReauth(account: Account, environment: Environment) async -> Bool {
        ConnectorLog.lifecycle("silentReauth start account=\(account.id)")
        var loginID: String?
        do {
            let start = try await connectorCLI.authStart(
                serverURL: environment.serverURLString,
                redirectURI: OAuthCallback.redirectURI,
                clientID: environment.clientID,
                issuer: nil,
                prompt: ConnectorCLI.silentPrompt,
                loginHint: account.email,
                configPath: configPath
            )
            loginID = start.loginID
            // `prompt=none` answers from the provider's own session: a code or
            // an `error=login_required` decline, never a login page.
            let attempt = await silentAuthorizationCode(for: start)
            guard let code = attempt.code else {
                return await abandonSilentReauth(account: account, loginID: loginID,
                                                 reason: attempt.decline ?? "no usable code")
            }

            let status = try await connectorCLI.authComplete(serverURL: environment.serverURLString,
                                                             loginID: start.loginID,
                                                             code: code,
                                                             state: start.state,
                                                             configPath: configPath)
            guard status.signedIn else {
                return await abandonSilentReauth(account: account, loginID: loginID,
                                                 reason: "the connector did not complete the sign-in")
            }

            // The episode is over: clearing the mark with the attempt means a
            // later rejection is a new one that tries again. The retried token
            // read is bounded separately (see `fetchAccessToken`), so this
            // cannot loop.
            clearNeedsReauth(account.id)
            sessionValid[account.id] = true
            if account.id == activeAccountID {
                setSessionState(.valid)
            }
            ConnectorLog.lifecycle("silentReauth succeeded account=\(account.id)")
            return true
        } catch {
            return await abandonSilentReauth(account: account, loginID: loginID,
                                             reason: error.localizedDescription)
        }
    }

    /// The provider interaction's answer: a code, or why it declined.
    private typealias SilentAuthorizationAttempt = (code: String?, decline: String?)

    /// Opens the `prompt=none` authorization URL and returns the code the
    /// provider issued, or the reason it declined. Bounded by
    /// `OAuthCallback.silentBrowserTimeout`, so an IdP that answers with HTML
    /// instead of a callback leaves no window up.
    private func silentAuthorizationCode(for start: CLIPKCEStart) async -> SilentAuthorizationAttempt {
        guard let authorizeURL = URL(string: start.authorizeURL) else {
            return (code: nil, decline: "invalid authorize URL")
        }
        let callback: URL
        do {
            callback = try await authenticator.authenticate(url: authorizeURL,
                                                            callbackURLScheme: OAuthCallback.callbackScheme,
                                                            timeout: OAuthCallback.silentBrowserTimeout)
        } catch {
            return (code: nil, decline: "no SSO session (\(error.localizedDescription))")
        }
        let items = OAuthCallback.queryItems(from: callback)
        if let error = items["error"] {
            return (code: nil, decline: error)
        }
        guard items["state"] == start.state, let code = items["code"], !code.isEmpty else {
            return (code: nil, decline: "callback carried no usable code")
        }
        return (code: code, decline: nil)
    }

    /// Leaves the account exactly as it was and discards the pending login, so
    /// a declined silent attempt cannot leave a half-started flow behind.
    @discardableResult
    private func abandonSilentReauth(account: Account, loginID: String?, reason: String) async -> Bool {
        ConnectorLog.lifecycle("silentReauth unavailable account=\(account.id) reason=\(reason)")
        if let loginID, !loginID.isEmpty {
            try? await connectorCLI.authCancel(loginID: loginID, configPath: configPath)
        }
        return false
    }
}
