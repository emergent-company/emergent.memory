import Combine
import Foundation

/// Errors surfaced by `AccountStore`.
enum AccountStoreError: LocalizedError, Equatable {
    case unknownAccount(String)

    var errorDescription: String? {
        switch self {
        case .unknownAccount(let id):
            return "No signed-in account with id \(id)."
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
final class AccountStore: ObservableObject {

    // MARK: - Constants

    nonisolated static let activeAccountIDKey = "connector.accounts.activeId"
    nonisolated static let accountsIndexFileName = "accounts.json"
    nonisolated static let accountsDirectoryName = "accounts"

    /// Default config root (shared with the single-account secret store).
    nonisolated static var defaultRootDirectory: URL { AppSecretStore.defaultBaseDirectory }

    // MARK: - Published state

    @Published private(set) var accounts: [Account] = []
    @Published private(set) var activeAccountID: String?
    /// Effective signed-in state: an account is active AND the connector CLI
    /// holds a valid session for it. The non-secret index (`accounts.json`)
    /// can outlive the CLI session, so this is deliberately NOT derived from
    /// `activeAccountID != nil`. Reconciled on bootstrap, sign-in, switch, and
    /// sign-out (and by `ensureConnectorSession`).
    @Published private(set) var isEffectivelySignedIn = false
    /// Per-account connector CLI session validity, keyed by account id. Absent
    /// until the account's session has been checked at least once.
    @Published private(set) var sessionValid: [String: Bool] = [:]
    /// Identity captured for an account, keyed by account id (in-memory only).
    /// Sign-in now runs in the CLI, so this is only populated by callers that
    /// still have a `OIDCUserInfo` to hand in.
    @Published private(set) var identities: [String: OIDCUserInfo] = [:]
    /// Accounts whose last token fetch failed — they need the user to sign in
    /// again. Other accounts are never touched by a failure here.
    @Published private(set) var needsReauth: Set<String> = []
    @Published private(set) var lastError: String?
    /// True while the browser sign-in flow is in flight (drives the UI spinner).
    @Published private(set) var isSigningIn = false

    // MARK: - Engine hooks

    /// Called before the active account changes: the caller stops/disconnects
    /// the engine for the previous account. Overridable in tests.
    var engineWillStop: @MainActor () -> Void = { EngineManager.shared.stop() }
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
    var connectorCLI: ConnectorCLI = ConnectorCLI()
    /// `--config` path passed to the connector CLI.
    var configPath: String = EngineManager.defaultConfigPath

    // MARK: - Init

    init(rootDirectory: URL = AccountStore.defaultRootDirectory,
         defaults: UserDefaults = .standard,
         authenticator: any BrowserAuthenticator = WebAuthenticator(),
         memoryClientFactory: @escaping @Sendable (String, String) -> MemoryAPIClient = { serverURL, token in
             MemoryAPIClient(serverURL: serverURL, token: token)
         }) {
        self.rootDirectory = rootDirectory
        self.defaults = defaults
        self.authenticator = authenticator
        self.memoryClientFactory = memoryClientFactory
        self.indexStore = AppSecretStore(baseDirectory: rootDirectory)
        self.accounts = Self.loadIndex(indexStore: indexStore)
        self.activeAccountID = Self.resolveActiveID(accounts: accounts, defaults: defaults)
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
    var isSignedIn: Bool { activeAccountID != nil }

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

            // Bridge the freshly established session through `auth import` so
            // the CLI config that `projects list` reads is guaranteed to carry
            // it. Best-effort: `auth complete` already stored the session, so a
            // failed bridge must not fail the sign-in.
            _ = await importSessionToConnectorCLI(environment: environment, email: status.email)

            let accountID = Self.accountID(environment: environment, email: status.email)
            let account = Account(id: accountID,
                                  environmentID: environment.id,
                                  email: status.email,
                                  displayName: nil,
                                  avatarObjectKey: nil)
            upsert(account)
            needsReauth.remove(accountID)
            persistIndex()

            if let previous = activeAccountID, previous != accountID {
                engineWillStop()
            }
            if activeAccountID != accountID {
                activeAccountID = accountID
                defaults.set(accountID, forKey: Self.activeAccountIDKey)
                activeAccountDidChange(account)
            }
            // The CLI just completed sign-in, so the session is valid now.
            sessionValid[accountID] = true
            isEffectivelySignedIn = true
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

    /// Recomputes `isEffectivelySignedIn` from the connector CLI's session for
    /// the active account. Call on bootstrap, sign-in, and account switch.
    /// Returns the reconciled value.
    @discardableResult
    func reconcileEffectiveSignedInState() async -> Bool {
        await ensureConnectorSession()
    }

    /// Re-asserts the connector CLI session for the active account, updating
    /// `isEffectivelySignedIn` / `sessionValid` as a side effect. When the CLI
    /// reports no valid session (the "app thinks signed in, CLI signed out"
    /// wedge), it makes one best-effort attempt to import the app's session,
    /// then re-checks. Returns true when the CLI ends signed in.
    @discardableResult
    func ensureConnectorSession() async -> Bool {
        guard let account = activeAccount, let environment = account.environment else {
            isEffectivelySignedIn = false
            return false
        }
        if await connectorIsSignedIn(environment: environment) {
            sessionValid[account.id] = true
            isEffectivelySignedIn = true
            return true
        }
        _ = await importSessionToConnectorCLI(environment: environment, email: account.email)
        let valid = await connectorIsSignedIn(environment: environment)
        sessionValid[account.id] = valid
        isEffectivelySignedIn = valid
        return valid
    }

    /// Marks the active account's session as no longer valid. Used by the app
    /// graph when a downstream CLI call (e.g. `projects list`) is rejected for
    /// auth reasons, so the account control flips to the signed-out affordance
    /// and never disagrees with the project/identity surfaces.
    func markActiveSessionInvalid() {
        guard let activeAccountID else {
            isEffectivelySignedIn = false
            return
        }
        sessionValid[activeAccountID] = false
        isEffectivelySignedIn = false
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
                                  email: String?) async -> Data? {
        guard let token = try? await connectorCLI.authAccessToken(serverURL: environment.serverURLString,
                                                                  configPath: configPath) else {
            return nil
        }
        var payload: [String: String] = ["access_token": token.accessToken]
        let expiry = token.expiresAt.trimmingCharacters(in: .whitespacesAndNewlines)
        if !expiry.isEmpty { payload["expires_at"] = expiry }
        let issuer = environment.issuerString.trimmingCharacters(in: .whitespacesAndNewlines)
        if !issuer.isEmpty { payload["issuer"] = issuer }
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
        } catch {
            throw Self.oidcError(from: error)
        }
        if activeAccountID != nil {
            engineWillStop()
        }
        activeAccountID = accountID
        defaults.set(accountID, forKey: Self.activeAccountIDKey)
        activeAccountDidChange(account)
        // The CLI validated this account's session above.
        sessionValid[accountID] = true
        isEffectivelySignedIn = true
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
        if wasActive { engineWillStop() }

        // Best-effort: drop the CLI-side session for this environment. Errors
        // (already signed out, CLI unavailable) are ignored.
        if let environment = account.environment {
            try? await connectorCLI.authLogout(serverURL: environment.serverURLString, configPath: configPath)
        }

        identities[accountID] = nil
        needsReauth.remove(accountID)
        sessionValid[accountID] = nil
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
    /// from the connector CLI (`auth access-token`, which refreshes server-side
    /// when near expiry). On failure only this account is marked as needing
    /// re-auth (`needsReauth`), never the others.
    ///
    /// Note: `needsReauthentication` stays a synchronous set updated here (and
    /// by `signIn`/`signOut`); querying the CLI is async, so a fully derived
    /// value would break the existing synchronous signature used by the UI.
    func currentAccessToken(for accountID: String) async throws -> String {
        guard let environment = account(id: accountID)?.environment else {
            needsReauth.insert(accountID)
            throw AccountStoreError.unknownAccount(accountID)
        }
        do {
            let token = try await connectorCLI.authAccessToken(serverURL: environment.serverURLString,
                                                               configPath: configPath)
            needsReauth.remove(accountID)
            sessionValid[accountID] = true
            if accountID == activeAccountID { isEffectivelySignedIn = true }
            return token.accessToken
        } catch {
            needsReauth.insert(accountID)
            sessionValid[accountID] = false
            if accountID == activeAccountID { isEffectivelySignedIn = false }
            throw Self.oidcError(from: error)
        }
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
        if email == nil, let me = try? await client.authMe() {
            email = me.email?.nilIfBlank ?? email
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
    nonisolated private static func needsIdentityBackfill(_ account: Account) -> Bool {
        account.email?.nilIfBlank == nil || account.displayName?.nilIfBlank == nil
    }

    nonisolated private static func combinedName(_ profile: UserProfile) -> String? {
        let parts = [profile.firstName?.nilIfBlank, profile.lastName?.nilIfBlank].compactMap { $0 }
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
              let index = try? JSONDecoder().decode(AccountsIndex.self, from: data) else {
            return []
        }
        return index.accounts
    }

    private static func resolveActiveID(accounts: [Account], defaults: UserDefaults) -> String? {
        if let id = defaults.string(forKey: activeAccountIDKey),
           accounts.contains(where: { $0.id == id }) {
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
        if let oidc = error as? OIDCError { return oidc }
        if let cli = error as? ConnectorCLIError {
            switch cli {
            case .binaryNotFound:
                return .invalidResponse("the bundled memory-connector CLI was not found")
            case .commandFailed(_, _, let message):
                return .invalidResponse(message)
            case .decodingFailed(_, let message):
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
