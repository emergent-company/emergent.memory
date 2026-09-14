import Combine
import Foundation

/// The app's single instance graph, shared by SwiftUI (via `.environmentObject`)
/// and AppKit (`StatusItemController`), so there is exactly ONE of each store.
///
/// `AccountStore` owns the signed-in accounts; the active account drives the
/// `ProjectStore` scope, identity, and the engine. All sign-in/token work is
/// delegated to the bundled `memory-connector` CLI.
@MainActor
final class AppEnvironment: ObservableObject {
    static let shared = AppEnvironment()

    let engine = EngineManager.shared
    let statusMonitor = StatusMonitor.shared
    let settings: ConnectorSettings
    let accountStore: AccountStore
    let projectStore: ProjectStore
    let appState = AppState()
    let identity = IdentityStore()

    /// The one-time legacy-session migrator, if one should run. Built by
    /// default against the shared legacy storage; `nil` when a migrator is
    /// injected as disabled or when hosted unit tests must not touch the real
    /// Keychain/session/config.
    let legacyMigrator: LegacyMigrator?

    /// Owned here so the app delegate can start/stop it around launch/quit.
    private(set) lazy var statusItemController = StatusItemController(environment: self)

    private var cancellables = Set<AnyCancellable>()

    /// - Parameter legacyMigrator: injectable override for tests. `nil` builds
    ///   the real migrator (unless the process is a hosted test run).
    init(legacyMigrator: LegacyMigrator? = nil) {
        let settings = ConnectorSettings()
        self.settings = settings
        let projectStore = ProjectStore(settings: settings)
        self.projectStore = projectStore
        let accountStore = AccountStore()
        self.accountStore = accountStore
        let migrator = legacyMigrator ?? Self.defaultLegacyMigrator()
        self.legacyMigrator = migrator

        // When a project load hits an auth failure, `ProjectStore` asks the
        // account store to re-establish the connector CLI session (a best-effort
        // `auth import`) before deciding whether to show the signed-out state.
        projectStore.repairConnectorSession = { [weak self] in
            guard let self else { return false }
            return await self.accountStore.ensureConnectorSession()
        }

        // Resolve organisation display names for the loaded projects so the
        // switcher groups by real organisation. Best-effort: any failure
        // (no token, `/api/orgs` error/empty) yields no names and grouping
        // falls back to "Other" without failing the project load.
        projectStore.organizationNameLoader = { [weak self] in
            guard let self,
                  let environment = self.accountStore.activeEnvironment,
                  let token = try? await self.accountStore.currentAccessToken() else {
                return [:]
            }
            let client = MemoryAPIClient(serverURL: environment.serverURLString, token: token)
            guard let orgs = try? await client.orgs() else { return [:] }
            return Dictionary(
                orgs.compactMap { org -> (String, String)? in
                    guard let name = org.name, !name.isEmpty else { return nil }
                    return (org.id, name)
                },
                uniquingKeysWith: { first, _ in first }
            )
        }

        // Keep the account control's effective signed-in state aligned with the
        // project surface: a project auth failure (`.signedOut`) also means the
        // active session is not usable, so the switcher must show Sign in.
        projectStore.$state
            .receive(on: RunLoop.main)
            .sink { [weak self] state in
                if case .signedOut = state {
                    self?.accountStore.markActiveSessionInvalid()
                }
            }
            .store(in: &cancellables)

        // Keep the engine + status polling aligned with the connected project.
        // `dropFirst()` skips the current value so init never starts anything;
        // only real connect/disconnect/sign-out transitions reconcile.
        projectStore.$connectedProjectID
            .dropFirst()
            .receive(on: RunLoop.main)
            .sink { [weak self] _ in self?.syncEngineWithConnection() }
            .store(in: &cancellables)

        // The active account drives the ProjectStore scope: switching accounts
        // swaps profiles/tokens/selection, stops the engine, and reloads data.
        accountStore.activeAccountDidChange = { [weak self] account in
            self?.handleActiveAccountChange(account)
        }

        // Restore the launch account's scope before any UI/engine work.
        if let account = accountStore.activeAccount {
            applyScope(for: account)
        }

        // One-time, best-effort legacy-session import into the connector CLI.
        // Kicked off once, non-blocking, and independent of sign-in state;
        // `migrateIfNeeded()` never throws.
        Task {
            await migrator?.migrateIfNeeded()
        }
    }

    /// Builds the real migrator against the shared legacy storage, or `nil`
    /// under hosted unit tests. Tests share the app process, so the default
    /// migrator must never touch the real Keychain/session/config from a test.
    private static func defaultLegacyMigrator() -> LegacyMigrator? {
        let env = ProcessInfo.processInfo.environment
        guard env["XCTestConfigurationFilePath"] == nil,
              env["XCTestBundlePath"] == nil else {
            return nil
        }
        return LegacyMigrator(
            cli: ConnectorCLI(),
            serverURLProvider: { ConnectorSettings.persistedServerURL() },
            reader: DefaultLegacySessionReader(),
            clearer: DefaultLegacySessionClearer(),
            defaults: .standard,
            configPath: EngineManager.defaultConfigPath)
    }

    /// Overall status from engine + hub state (same derivation as before).
    var appStatus: AppStatus {
        AppStatus.derive(engine: engine.state, snapshot: statusMonitor.snapshot)
    }

    // MARK: - Account scope

    /// Reacts to the active account changing (sign-in, switch, sign-out).
    private func handleActiveAccountChange(_ account: Account?) {
        guard let account else {
            // Last account signed out → stop the engine, clear the project
            // connection, and drop any identity.
            projectStore.stopAndClear()
            identity.reset()
            return
        }
        applyScope(for: account)
        Task { await reloadActiveAccountData() }
    }

    /// Points `ProjectStore` at `account`'s storage/environment and reconciles
    /// the engine so only this account's connected project can run.
    private func applyScope(for account: Account) {
        guard let scope = accountStore.projectScope(for: account.id) else { return }
        projectStore.applyScope(scope)
        // Rewrite the shared engine config from this account's own CLI session
        // so a previous account's config can never linger into this scope, then
        // reconcile the engine. The CLI call is async, so defer it.
        Task { [weak self] in
            guard let self else { return }
            _ = await self.projectStore.reassertConnection()
            self.syncEngineWithConnection()
        }
    }

    /// Loads the active account's projects (which also resolves organisations)
    /// and identity, then reconciles the engine.
    private func reloadActiveAccountData() async {
        guard accountStore.activeAccount != nil else { return }
        // Reconcile the effective signed-in state from the connector CLI
        // session (self-healing a wedged index via a best-effort session
        // import) so the account control and the project/identity surfaces
        // agree. Then load via the CLI, which `ProjectStore.loadProjects`
        // attempts even when the app-side access token cannot be fetched. On an
        // auth failure it re-imports the session and, if the CLI is still
        // unsigned, surfaces `.signedOut` instead of a raw error; other
        // failures stay `.error`.
        await accountStore.reconcileEffectiveSignedInState()
        let token = try? await accountStore.currentAccessToken()
        await projectStore.loadProjects(accessToken: token ?? "")
        if let token {
            await identity.load(serverURL: accountStore.activeEnvironment?.serverURLString ?? settings.serverURL,
                                token: token)
        }
        syncEngineWithConnection()
    }

    // MARK: - Engine lifecycle gating

    /// The single source of truth for "may the engine run?" — a project must be
    /// connected AND its engine config must exist. Testable; used at launch and
    /// after sign-in/bootstrap.
    func shouldStartEngine() -> Bool {
        EngineLifecyclePolicy.shouldRun(connectedProjectID: projectStore.connectedProjectID,
                                        configURL: EngineConfigSync.configURL)
    }

    /// Reconciles the engine process with the connected-project state:
    ///
    /// - connected + config exists → ensure it is running;
    /// - no connected project → stop it (a stale config must never reconnect);
    /// - connected but config missing → stop and surface an error (never start
    ///   a stale/other project's config).
    ///
    /// Safe to call repeatedly (`start()` no-ops while running, `stop()` is
    /// idempotent).
    func syncEngineWithConnection() {
        let decision = EngineLifecyclePolicy.decision(connectedProjectID: projectStore.connectedProjectID,
                                                      configURL: EngineConfigSync.configURL)
        switch decision {
        case .run:
            engine.start()
            statusMonitor.start()
        case .noConnectedProject:
            engine.stop()
            statusMonitor.stop()
            statusMonitor.markStopped()
        case .missingConfig(let projectID):
            engine.stop()
            statusMonitor.stop()
            statusMonitor.markStopped()
            engine.reportConfigurationError(
                "Engine config for connected project \(projectID) is missing; not starting.")
        }
    }

    /// Launch/session bootstrap: when an account is signed in, load its project
    /// list and refresh identity so the toolbar picker is populated without the
    /// user visiting a page. Safe to call repeatedly.
    func bootstrap() async {
        guard accountStore.isSignedIn else { return }
        await reloadActiveAccountData()
        // Restored account index entries may predate identity capture (or a
        // provider may not surface it) — fill in name/email in the
        // background, best-effort.
        await accountStore.backfillIdentityIfNeeded()
    }
}
