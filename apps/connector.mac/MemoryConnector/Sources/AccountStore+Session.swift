import Foundation

/// Connector CLI session reconciliation and the state transitions that follow
/// from it.
///
/// `AccountStore.swift` owns the session fields; this extension decides what
/// they mean. The distinction that matters throughout is *dead vs. unreachable*:
/// only a provider rejection ends a session, an unreachable CLI is retried.
extension AccountStore {
    // MARK: - Connector CLI session bridge

    /// Recomputes the effective signed-in state from the connector CLI's session
    /// for the active account. Call on bootstrap, sign-in, and account switch.
    /// Returns the reconciled value.
    @discardableResult
    func reconcileEffectiveSignedInState() async -> Bool {
        await ensureConnectorSession()
    }

    /// Re-asserts the connector CLI session for the active account, updating
    /// `sessionState` / `sessionValid` as a side effect.
    ///
    /// The order matters. A stored session whose access token has expired is
    /// **renewed before anything is reported to the UI**, so a launch after a
    /// long sleep (expired access token, perfectly good refresh token) comes
    /// back signed in silently. Only a rejection from the provider, or a
    /// genuinely absent session, ends in `.needsReauth`; an unreachable CLI ends
    /// in `.offline`, which keeps the signed-in chrome — that is a retry, not a
    /// sign-out.
    ///
    /// When the CLI reports no session at all (the historical "app thinks
    /// signed in, CLI signed out" wedge), it still makes one best-effort import
    /// and re-checks. Returns true when the CLI ends signed in.
    @discardableResult
    func ensureConnectorSession() async -> Bool {
        guard let account = activeAccount, let environment = account.environment else {
            setSessionState(.unknown)
            return false
        }
        setSessionState(.refreshing)

        guard let status = try? await connectorCLI.authStatus(serverURL: environment.serverURLString,
                                                              configPath: configPath)
        else {
            // The CLI itself is unreachable (missing binary, killed process,
            // timeout). The stored session is untouched — keep the chrome and
            // let the next check decide.
            setSessionState(.offline)
            return false
        }

        if status.needsReauth == true {
            // The provider already rejected the stored refresh token. Before
            // asking the user anything, try the non-interactive path: a machine
            // that still has a live SSO session signs itself back in.
            if await attemptSilentReauth(for: account.id) {
                return true
            }
            markNeedsReauth(account.id)
            return false
        }

        if status.signedIn {
            if status.expired == true, status.canRefresh == true {
                switch await renewSession(environment: environment, accountID: account.id) {
                case .renewed:
                    break
                case .transient:
                    // Stay signed in: the stored refresh token is still there,
                    // the next attempt may well work.
                    sessionValid[account.id] = true
                    setSessionState(.offline)
                    return false
                case .rejected:
                    return false
                }
            }
            // The CLI reports the session as usable, so any stale re-auth mark
            // on this account is out of date.
            clearNeedsReauth(account.id)
            sessionValid[account.id] = true
            setSessionState(.valid)
            return true
        }

        _ = await importSessionToConnectorCLI(environment: environment, email: account.email)
        let valid = await connectorIsSignedIn(environment: environment)
        sessionValid[account.id] = valid
        if valid {
            clearNeedsReauth(account.id)
            setSessionState(.valid)
        } else {
            markNeedsReauth(account.id)
        }
        return valid
    }

    /// True when the connector CLI reports a valid session for `environment`.
    func connectorIsSignedIn(environment: Environment) async -> Bool {
        let status = try? await connectorCLI.authStatus(serverURL: environment.serverURLString,
                                                        configPath: configPath)
        return status?.signedIn == true
    }

    /// Pushes the app's session for `environment` into the connector CLI via
    /// `auth import` (snake_case JSON on stdin). This is the bridge that makes
    /// a fresh sign-in — and a self-heal on bootstrap — visible to everything
    /// the CLI serves, including `projects list`. Best-effort: returns true
    /// only when the CLI reports signed in afterwards.
    ///
    /// The connector CLI now owns the OIDC session, so the document is rebuilt
    /// from the CLI's own stored access token plus the known environment issuer
    /// and account email. When there is no readable session at all, there is
    /// nothing to import and this returns false (the caller then shows the
    /// signed-out state).
    @discardableResult
    func importSessionToConnectorCLI(environment: Environment, email: String?) async -> Bool {
        guard let document = await connectorSessionDocument(environment: environment, email: email) else {
            return false
        }
        let status = try? await connectorCLI.authImport(serverURL: environment.serverURLString,
                                                        sessionJSON: document,
                                                        configPath: configPath)
        return status?.signedIn == true
    }

    /// Builds the snake_case `auth import` stdin document from the CLI's stored
    /// access token, the environment issuer, and the account email. Optional
    /// fields are omitted when empty so the CLI applies its own defaults.
    func connectorSessionDocument(environment: Environment,
                                  email: String?) async -> Data?
    {
        guard let token = try? await connectorCLI.authAccessToken(serverURL: environment.serverURLString,
                                                                  configPath: configPath)
        else {
            return nil
        }
        var payload: [String: String] = ["access_token": token.accessToken]
        let expiry = token.expiresAt.trimmingCharacters(in: .whitespacesAndNewlines)
        if !expiry.isEmpty {
            payload["expires_at"] = expiry
        }
        let issuer = environment.issuerString.trimmingCharacters(in: .whitespacesAndNewlines)
        if !issuer.isEmpty {
            payload["issuer"] = issuer
        }
        if let email = email?.trimmingCharacters(in: .whitespacesAndNewlines), !email.isEmpty {
            payload["email"] = email
        }
        return try? JSONSerialization.data(withJSONObject: payload, options: [.sortedKeys])
    }

    // MARK: - Session invalidation and renewal

    /// Marks the active account's session as no longer valid. Used by the app
    /// graph when a downstream CLI call (e.g. `projects list`) is rejected for
    /// auth reasons, so the account control flips to the signed-out affordance
    /// and never disagrees with the project/identity surfaces.
    func markActiveSessionInvalid() {
        guard let activeAccountID else {
            setSessionState(.unknown)
            return
        }
        markNeedsReauth(activeAccountID)
    }

    /// Records that `accountID` needs a new sign-in, and reflects it in the
    /// session state when that account is the active one.
    func markNeedsReauth(_ accountID: String) {
        needsReauth.insert(accountID)
        sessionValid[accountID] = false
        tokenCache[accountID] = nil
        if accountID == activeAccountID {
            setSessionState(.needsReauth)
        }
    }

    /// Clears the mark together with the attempt that consumed its episode:
    /// leaving `needsReauth` is what ends the episode, so the next rejection is
    /// a new one that may try again.
    func clearNeedsReauth(_ accountID: String) {
        needsReauth.remove(accountID)
        silentReauthAttempted.remove(accountID)
    }

    /// Updates `sessionState`, logging transitions only (a poll loop that keeps
    /// reporting the same state must not fill the log).
    func setSessionState(_ state: ConnectorSessionState) {
        guard sessionState != state else { return }
        ConnectorLog.lifecycle("sessionState \(sessionState.rawValue) -> \(state.rawValue)")
        sessionState = state
    }

    /// Outcome of a renewal attempt: only `.rejected` means the session is dead.
    private enum RenewalOutcome {
        case renewed
        case transient
        case rejected
    }

    /// Renews the stored session and reports what happened. The CLI keeps the
    /// session (and marks it `needs_reauth`) when the provider rejects the
    /// refresh token, so a failure is disambiguated by asking for the status
    /// rather than by matching error text.
    private func renewSession(environment: Environment, accountID: String) async -> RenewalOutcome {
        do {
            let refresh = try await connectorCLI.authRefresh(serverURL: environment.serverURLString,
                                                             configPath: configPath)
            if refresh.needsReauth == true {
                markNeedsReauth(accountID)
                return .rejected
            }
            // The CLI renewed it; drop any cached token so the next read takes
            // the fresh one.
            tokenCache[accountID] = nil
            return .renewed
        } catch {
            if await isAuthRejection(error: error, environment: environment) {
                markNeedsReauth(accountID)
                return .rejected
            }
            return .transient
        }
    }

    /// True when a token-read failure means the session is genuinely dead rather
    /// than temporarily unreachable.
    ///
    /// The CLI's own message is the first signal (`not signed in`, an OAuth
    /// code), but it does not always carry one: a rejected refresh surfaces as
    /// "refresh failed with status 400" while the CLI has already recorded
    /// `needs_reauth` on the stored session. Asking the status is therefore the
    /// authority, and it costs one extra process spawn only on the failure path.
    /// A status the CLI cannot produce at all is not evidence of a dead session.
    func isAuthRejection(error: Error, environment: Environment) async -> Bool {
        if AuthFailure.looksLikeAuthFailure(error) {
            return true
        }
        let status = try? await connectorCLI.authStatus(serverURL: environment.serverURLString,
                                                        configPath: configPath)
        return status?.needsReauth == true
    }
}
