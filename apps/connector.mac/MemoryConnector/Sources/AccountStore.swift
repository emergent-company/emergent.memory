import Foundation
import Observation

/// Errors surfaced by `AccountStore`.
enum AccountStoreError: LocalizedError, Equatable {
    case unknownAccount(String)

    var errorDescription: String? {
        switch self {
        case let .unknownAccount(id):
            "No signed-in account with id \(id)."
        }
    }
}

/// The non-secret account index persisted as `accounts.json`.
struct AccountsIndex: Codable, Equatable, Sendable {
    var accounts: [Account]
}

/// Owns the signed-in account list, the active account, and per-account secret
/// directories.
///
/// Sign-in, access tokens, and sign-out all run through the bundled
/// `memory-connector` CLI (`ConnectorCLI`), which owns PKCE, the token
/// exchange, refresh, session storage, and connector tokens. The app keeps only
/// the non-secret account index and each account's 0600 directory for its
/// project profiles.
///
/// Storage layout under the config root (`~/.config/memory-connector/` by
/// default, injectable for tests):
///
/// ```
/// accounts.json                          # non-secret index (0600)
/// accounts/<accountID>/profiles.json     # per-account project profiles
/// ```
///
/// `activeAccountID` is persisted in `UserDefaults`. Every secret read/write
/// goes through an `AppSecretStore` rooted at that account's directory, so
/// switching accounts is a scope swap rather than a shared mutation.
@MainActor
@Observable
final class AccountStore {
    // MARK: - Constants

    nonisolated static let activeAccountIDKey = "connector.accounts.activeId"
    nonisolated static let accountsIndexFileName = "accounts.json"
    nonisolated static let accountsDirectoryName = "accounts"

    /// Default config root (shared with the single-account secret store).
    nonisolated static var defaultRootDirectory: URL {
        AppSecretStore.defaultBaseDirectory
    }

    // MARK: - Observable state

    private(set) var accounts: [Account] = []
    private(set) var activeAccountID: String?
    /// The connector CLI session state for the active account. This
    /// deliberately distinguishes "cannot reach the CLI right now" (`offline`)
    /// from "the provider rejected the session" (`needsReauth`): the first must
    /// never sign the user out. Reconciled on bootstrap, sign-in, switch,
    /// sign-out, and every token read.
    private(set) var sessionState: ConnectorSessionState = .unknown
    /// Effective signed-in state: an account is active AND its session is not
    /// known to be dead. The non-secret index (`accounts.json`) can outlive the
    /// CLI session, so this is deliberately NOT derived from
    /// `activeAccountID != nil` alone.
    var isEffectivelySignedIn: Bool {
        sessionState.keepsSignedIn && activeAccountID != nil
    }

    /// Per-account connector CLI session validity, keyed by account id. Absent
    /// until the account's session has been checked at least once.
    private(set) var sessionValid: [String: Bool] = [:]
    /// Identity captured for an account, keyed by account id (in-memory only).
    /// Sign-in now runs in the CLI, so this is only populated by callers that
    /// still have a `OIDCUserInfo` to hand in.
    private(set) var identities: [String: OIDCUserInfo] = [:]
    /// Accounts whose last token fetch failed — they need the user to sign in
    /// again. Other accounts are never touched by a failure here.
    private(set) var needsReauth: Set<String> = []
    private(set) var lastError: String?
    /// True while the browser sign-in flow is in flight (drives the UI spinner).
    private(set) var isSigningIn = false

    // MARK: - Engine hooks

    /// Called before the active account changes: the caller stops/disconnects
    /// the engine for the previous account. Overridable in tests.
    var engineWillStop: @MainActor () -> Void = {}
    /// Called after the active account changes (or is cleared).
    var activeAccountDidChange: @MainActor (Account?) -> Void = { _ in }

    // MARK: - Dependencies

    private let rootDirectory: URL
    private let defaults: UserDefaults
    private let authenticator: any BrowserAuthenticator
    private let indexStore: AppSecretStore
    /// Builds a `MemoryAPIClient` for a (serverURL, accessToken) pair used by
    /// identity backfill. Injectable so tests can stub the network.
    private let memoryClientFactory: @Sendable (String, String) -> MemoryAPIClient

    // MARK: - Connector CLI seam

    //
    // Sign-in, access tokens, and sign-out run through the bundled
    // `memory-connector` CLI. Injectable so tests can supply a scripted runner;
    // `configPath` is the `--config` path the CLI reads/writes (defaults to the
    // shared engine config).

    /// The CLI wrapper used for sign-in/token/logout. Tests assign a
    /// `ConnectorCLI` built with a fake runner.
    var connectorCLI: ConnectorCLI = .init()
    /// `--config` path passed to the connector CLI.
    var configPath: String = EngineManager.defaultConfigPath

    // MARK: - Access token cache

    /// A token returned by the CLI plus its expiry, kept in memory only.
    private struct CachedToken: Sendable {
        let accessToken: String
        let expiresAt: Date
    }

    /// Last token read per account. Every read spawns a `memory-connector`
    /// process, and one launch reads a token several times (dashboard, engine
    /// config, project list, org names), so a short cache removes a burst of
    /// process spawns.
    private var tokenCache: [String: CachedToken] = [:]
    /// In-flight token reads per account: concurrent callers await the same
    /// task instead of racing their own copy.
    private var tokenTasks: [String: Task<String, Error>] = [:]

    // MARK: - Silent re-auth state

    /// In-flight non-interactive re-auth per account: single-flight, so a
    /// launch, a wake and a token read that all notice `needs_reauth` at once
    /// produce ONE authorization attempt, not three.
    private var silentReauthTasks: [String: Task<Bool, Never>] = [:]
    /// Accounts that have had their one silent attempt for the current
    /// `needs_reauth` episode; cleared only on positive evidence of a working
    /// session (a token read, a usable status, sign-in, a switch), never by the
    /// attempt itself, so a failed attempt cannot trigger the next one.
    private var silentReauthAttempted: Set<String> = []

    /// A cached token is reused only while it keeps at least this much life: a
    /// token that expires inside the floor is renewed rather than handed to an
    /// API call that would fail moments later.
    nonisolated static let tokenCacheFloor: TimeInterval = 60

    // MARK: - Init

    init(rootDirectory: URL = AccountStore.defaultRootDirectory,
         defaults: UserDefaults = .standard,
         authenticator: any BrowserAuthenticator = WebAuthenticator(),
         memoryClientFactory: @escaping @Sendable (String, String) -> MemoryAPIClient = { serverURL, token in
             MemoryAPIClient(serverURL: serverURL, token: token)
         })
    {
        self.rootDirectory = rootDirectory
        self.defaults = defaults
        self.authenticator = authenticator
        self.memoryClientFactory = memoryClientFactory
        indexStore = AppSecretStore(baseDirectory: rootDirectory)
        accounts = Self.loadIndex(indexStore: indexStore)
        activeAccountID = Self.resolveActiveID(accounts: accounts, defaults: defaults)
    }

    // MARK: - Derived accessors

    var activeAccount: Account? {
        guard let activeAccountID else { return nil }
        return account(id: activeAccountID)
    }

    var activeEnvironment: Environment? {
        activeAccount?.environment
    }

    /// True when the NON-secret index has an active account. This can outlive
    /// the session, so it must not drive signed-in UI — use
    /// `isEffectivelySignedIn` for that.
    var isSignedIn: Bool {
        activeAccountID != nil
    }

    /// The identity captured for the active account, when known (in-memory; the
    /// account index is the fallback after a relaunch).
    var activeIdentity: OIDCUserInfo? {
        activeAccountID.flatMap { identities[$0] }
    }

    func account(id: String) -> Account? {
        accounts.first { $0.id == id }
    }

    func environment(for accountID: String) -> Environment? {
        account(id: accountID)?.environment
    }

    func identity(for accountID: String) -> OIDCUserInfo? {
        identities[accountID]
    }

    func needsReauthentication(_ accountID: String) -> Bool {
        needsReauth.contains(accountID)
    }

    // MARK: - Per-account storage

    /// The 0600 secret directory for an account.
    func accountDirectory(for accountID: String) -> URL {
        rootDirectory
            .appendingPathComponent(Self.accountsDirectoryName, isDirectory: true)
            .appendingPathComponent(Self.directoryName(for: accountID), isDirectory: true)
    }

    /// `AppSecretStore` rooted at the account's own directory.
    func secrets(for accountID: String) -> AppSecretStore {
        AppSecretStore(baseDirectory: accountDirectory(for: accountID))
    }

    /// The `ProjectStoreScope` that binds `ProjectStore` to this account's
    /// profiles, tokens, environment and active-project key.
    func projectScope(for accountID: String) -> ProjectStoreScope? {
        guard let account = account(id: accountID),
              let environment = account.environment else { return nil }
        return ProjectStoreScope.account(account, environment: environment, rootDirectory: rootDirectory)
    }

    // MARK: - Sign in

    /// Runs the connector CLI two-step PKCE flow against `environment`, upserts
    /// the account in the index, and makes it active. Returns the (new or
    /// existing) account.
    ///
    /// The CLI (`auth start` / `auth complete`) owns PKCE, the token exchange,
    /// and session storage; the app only opens the returned `authorize_url` and
    /// relays the callback's `code`/`state`.
    @discardableResult
    func signIn(environment: Environment) async throws -> Account {
        lastError = nil
        isSigningIn = true
        defer { isSigningIn = false }
        do {
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

            let accountID = Self.accountID(environment: environment, email: status.email)
            let account = Account(id: accountID,
                                  environmentID: environment.id,
                                  email: status.email,
                                  displayName: nil,
                                  avatarObjectKey: nil)
            upsert(account)
            needsReauth.remove(accountID)
            clearSilentReauthAttempt(accountID)
            persistIndex()

            if let previous = activeAccountID, previous != accountID {
                engineWillStop()
            }
            if activeAccountID != accountID {
                activeAccountID = accountID
                defaults.set(accountID, forKey: Self.activeAccountIDKey)
                ConnectorLog.lifecycle("activeAccountDidChange source=signIn account=\(account.id)")
                activeAccountDidChange(account)
            }
            // The CLI just completed sign-in, so the session is valid now.
            sessionValid[accountID] = true
            tokenCache[accountID] = nil
            setSessionState(.valid)
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
            needsReauth.remove(account.id)
            clearSilentReauthAttempt(account.id)
            sessionValid[account.id] = true
            setSessionState(.valid)
            return true
        }

        _ = await importSessionToConnectorCLI(environment: environment, email: account.email)
        let valid = await connectorIsSignedIn(environment: environment)
        sessionValid[account.id] = valid
        if valid {
            needsReauth.remove(account.id)
            clearSilentReauthAttempt(account.id)
            setSessionState(.valid)
        } else {
            markNeedsReauth(account.id)
        }
        return valid
    }

    // MARK: - Silent re-auth

    /// Non-interactive re-auth for an account whose stored session the provider
    /// rejected (`needs_reauth`).
    ///
    /// It runs the SAME `auth start`/`auth complete` pair the interactive flow
    /// uses, with `prompt=none` and the account's email as `login_hint`, so a
    /// machine still signed in at the identity provider comes back without a
    /// login page. A decline ("login_required", a cancelled browser session,
    /// anything else) cancels the pending login and changes nothing: the
    /// `needs_reauth` mark and the interactive affordance stay, and `lastError`
    /// is never written because the user neither caused nor can act on it.
    ///
    /// At most ONE attempt runs per `needs_reauth` episode: the attempt is
    /// recorded before it starts and cleared only when a session is positively
    /// observed working again, so two surfaces noticing the same rejection share
    /// one attempt instead of starting a sign-in loop.
    ///
    /// - Returns: true when the attempt restored a usable session.
    @discardableResult
    func attemptSilentReauth(for accountID: String) async -> Bool {
        guard let account = account(id: accountID), let environment = account.environment else {
            return false
        }
        // Join an attempt already in flight first: it is running *because* this
        // episode has no attempt recorded yet, and its caller must see the
        // result the other surface is about to observe.
        if let inFlight = silentReauthTasks[accountID] {
            return await inFlight.value
        }
        guard !silentReauthAttempted.contains(accountID) else { return false }
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
            // `prompt=none` resolves from the provider's own session: no login
            // page is rendered and the callback arrives immediately, either
            // with a code or with an `error=login_required`-style decline.
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

            needsReauth.remove(account.id)
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
    /// provider issued, or the reason it declined. Split from `runSilentReauth`
    /// so that function reads as the flow (start → authorize → complete).
    private func silentAuthorizationCode(for start: CLIPKCEStart) async -> SilentAuthorizationAttempt {
        guard let authorizeURL = URL(string: start.authorizeURL) else {
            return (code: nil, decline: "invalid authorize URL")
        }
        let callback: URL
        do {
            callback = try await authenticator.authenticate(url: authorizeURL,
                                                            callbackURLScheme: OAuthCallback.callbackScheme)
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

    /// Allows the next `needs_reauth` episode to attempt a silent re-auth again.
    /// Only a positively working session clears it (see
    /// `silentReauthAttempted`).
    private func clearSilentReauthAttempt(_ accountID: String) {
        silentReauthAttempted.remove(accountID)
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
    private func markNeedsReauth(_ accountID: String) {
        needsReauth.insert(accountID)
        sessionValid[accountID] = false
        tokenCache[accountID] = nil
        if accountID == activeAccountID {
            setSessionState(.needsReauth)
        }
    }

    /// Updates `sessionState`, logging transitions only (a poll loop that keeps
    /// reporting the same state must not fill the log).
    private func setSessionState(_ state: ConnectorSessionState) {
        guard sessionState != state else { return }
        ConnectorLog.lifecycle("sessionState \(sessionState.rawValue) -> \(state.rawValue)")
        sessionState = state
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
        if activeAccountID != nil {
            engineWillStop()
        }
        activeAccountID = accountID
        defaults.set(accountID, forKey: Self.activeAccountIDKey)
        ConnectorLog.lifecycle("activeAccountDidChange source=switchTo account=\(account.id)")
        activeAccountDidChange(account)
        // The CLI validated this account's session above.
        needsReauth.remove(accountID)
        clearSilentReauthAttempt(accountID)
        sessionValid[accountID] = true
        tokenCache[accountID] = nil
        setSessionState(.valid)
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
        needsReauth.remove(accountID)
        clearSilentReauthAttempt(accountID)
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
    private func fetchAccessToken(for accountID: String) async throws -> String {
        guard let environment = account(id: accountID)?.environment else {
            markNeedsReauth(accountID)
            throw AccountStoreError.unknownAccount(accountID)
        }
        do {
            let token = try await connectorCLI.authAccessToken(serverURL: environment.serverURLString,
                                                               configPath: configPath)
            needsReauth.remove(accountID)
            clearSilentReauthAttempt(accountID)
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
                // flow may fix without asking: try `prompt=none` once, then
                // read the (now renewed) token. The attempt guard is what stops
                // a second failure from starting a second attempt.
                if await attemptSilentReauth(for: accountID) {
                    return try await fetchAccessToken(for: accountID)
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

    /// True when a token-read failure means the session is genuinely dead rather
    /// than temporarily unreachable.
    ///
    /// The CLI's own message is the first signal (`not signed in`, an OAuth
    /// code), but it does not always carry one: a rejected refresh surfaces as
    /// "refresh failed with status 400" while the CLI has already recorded
    /// `needs_reauth` on the stored session. Asking the status is therefore the
    /// authority, and it costs one extra process spawn only on the failure path.
    /// A status the CLI cannot produce at all is not evidence of a dead session.
    private func isAuthRejection(error: Error, environment: Environment) async -> Bool {
        if AuthFailure.looksLikeAuthFailure(error) {
            return true
        }
        let status = try? await connectorCLI.authStatus(serverURL: environment.serverURLString,
                                                        configPath: configPath)
        return status?.needsReauth == true
    }

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

    // MARK: - Index persistence

    private func upsert(_ account: Account) {
        if let index = accounts.firstIndex(where: { $0.id == account.id }) {
            accounts[index] = account
        } else {
            accounts.append(account)
        }
    }

    private func persistIndex() {
        let index = AccountsIndex(accounts: accounts)
        guard let data = try? JSONEncoder().encode(index) else { return }
        try? indexStore.writeData(data, to: Self.accountsIndexFileName)
    }

    nonisolated static func loadIndex(indexStore: AppSecretStore) -> [Account] {
        guard let data = indexStore.readData(accountsIndexFileName),
              let index = try? JSONDecoder().decode(AccountsIndex.self, from: data)
        else {
            return []
        }
        return index.accounts
    }

    private static func resolveActiveID(accounts: [Account], defaults: UserDefaults) -> String? {
        if let id = defaults.string(forKey: activeAccountIDKey),
           accounts.contains(where: { $0.id == id })
        {
            return id
        }
        return accounts.first?.id
    }

    nonisolated static func directoryName(for accountID: String) -> String {
        accountID.replacingOccurrences(of: "/", with: "_")
    }

    // MARK: - Identity

    /// `"<environmentID>:<email>"` for the CLI sign-in path (the CLI status
    /// carries only an email). Falls back to `"unknown"` when absent.
    nonisolated static func accountID(environment: Environment, email: String?) -> String {
        let suffix = email?.nilIfBlank ?? "unknown"
        return "\(environment.id):\(suffix)"
    }

    /// Maps connector CLI failures onto the `OIDCError` vocabulary the app
    /// already surfaces; other errors (including `OIDCError`) pass through.
    nonisolated static func oidcError(from error: Error) -> OIDCError {
        if let oidc = error as? OIDCError {
            return oidc
        }
        if let cli = error as? ConnectorCLIError {
            switch cli {
            case .binaryNotFound:
                return .invalidResponse("the bundled memory-connector CLI was not found")
            case let .commandFailed(_, _, message):
                return .invalidResponse(message)
            case let .decodingFailed(_, message):
                return .invalidResponse(message)
            }
        }
        return .invalidResponse(error.localizedDescription)
    }
}

private extension String {
    /// The trimmed string, or nil when blank.
    var nilIfBlank: String? {
        let trimmed = trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }
}
