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
///
/// This declaration holds state, dependencies and the object lifecycle only.
/// The behaviour is split by concern across `AccountStore+*.swift` extension
/// files — accounts/index, sign-in, session reconciliation, silent re-auth,
/// token caching, identity backfill — so no single file reaches the
/// `file_length` threshold or turns the baselined `type_body_length` warning
/// into an error.
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

    //
    // These are `internal` rather than `private(set)`: Swift's `private` is
    // scoped to the declaring FILE, and the `AccountStore+*.swift` extensions
    // that own most of the mutations live in other files. The class is `final`
    // and module-internal, so this does not widen the app's API surface — the
    // extension files are the only writers.

    var accounts: [Account] = []
    var activeAccountID: String?
    /// The connector CLI session state for the active account. This
    /// deliberately distinguishes "cannot reach the CLI right now" (`offline`)
    /// from "the provider rejected the session" (`needsReauth`): the first must
    /// never sign the user out. Reconciled on bootstrap, sign-in, switch,
    /// sign-out, and every token read.
    var sessionState: ConnectorSessionState = .unknown
    /// Effective signed-in state: an account is active AND its session is not
    /// known to be dead. The non-secret index (`accounts.json`) can outlive the
    /// CLI session, so this is deliberately NOT derived from
    /// `activeAccountID != nil` alone.
    var isEffectivelySignedIn: Bool {
        sessionState.keepsSignedIn && activeAccountID != nil
    }

    /// Per-account connector CLI session validity, keyed by account id. Absent
    /// until the account's session has been checked at least once.
    var sessionValid: [String: Bool] = [:]
    /// Identity captured for an account, keyed by account id (in-memory only).
    /// Sign-in now runs in the CLI, so this is only populated by callers that
    /// still have a `OIDCUserInfo` to hand in.
    var identities: [String: OIDCUserInfo] = [:]
    /// Accounts whose last token fetch failed — they need the user to sign in
    /// again. Other accounts are never touched by a failure here.
    ///
    /// Membership is what scopes the silent-re-auth attempt mark: the mark only
    /// counts while the account is marked (see `attemptSilentReauth`), so
    /// leaving this set — on any positive evidence — ends the episode and
    /// allows the next one to try again.
    var needsReauth: Set<String> = []
    var lastError: String?
    /// True while the browser sign-in flow is in flight (drives the UI spinner).
    var isSigningIn = false

    // MARK: - Engine hooks

    /// Called before the active account changes: the caller stops/disconnects
    /// the engine for the previous account. Overridable in tests.
    var engineWillStop: @MainActor () -> Void = {}
    /// Called after the active account changes (or is cleared).
    var activeAccountDidChange: @MainActor (Account?) -> Void = { _ in }

    // MARK: - Dependencies

    //
    // Internal for the same reason as the state above: the extension files read
    // them. Nothing outside `AccountStore` assigns them.

    let rootDirectory: URL
    let defaults: UserDefaults
    let authenticator: any BrowserAuthenticator
    let indexStore: AppSecretStore
    /// Builds a `MemoryAPIClient` for a (serverURL, accessToken) pair used by
    /// identity backfill. Injectable so tests can stub the network.
    let memoryClientFactory: @Sendable (String, String) -> MemoryAPIClient

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
    struct CachedToken: Sendable {
        let accessToken: String
        let expiresAt: Date
    }

    /// Last token read per account. Every read spawns a `memory-connector`
    /// process, and one launch reads a token several times (dashboard, engine
    /// config, project list, org names), so a short cache removes a burst of
    /// process spawns.
    var tokenCache: [String: CachedToken] = [:]
    /// In-flight token reads per account: concurrent callers await the same
    /// task instead of racing their own copy.
    var tokenTasks: [String: Task<String, Error>] = [:]

    // MARK: - Silent re-auth state

    /// In-flight non-interactive re-auth per account: single-flight, so a launch,
    /// a wake and a token read that notice `needs_reauth` run ONE attempt.
    var silentReauthTasks: [String: Task<Bool, Never>] = [:]
    /// Accounts that already attempted a silent re-auth in the current
    /// `needs_reauth` episode. Set before the attempt, so a FAILED attempt
    /// cannot retrigger it, and cleared with `needsReauth` (see
    /// `clearNeedsReauth`), which is what ends the episode.
    var silentReauthAttempted: Set<String> = []

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
}
