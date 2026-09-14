import Combine
import Foundation

/// Binds `ProjectStore` to one account's storage + environment.
///
/// The shared/nil-account scope keeps the legacy single-account behaviour
/// (profiles in `UserDefaults`, tokens in the shared secret dir). An account
/// scope points profiles/tokens at the account's own 0600 directory under
/// `accounts/<id>/` and namespaces the persisted active-project key, so two
/// accounts can never see each other's projects, tokens or connection flag.
struct ProjectStoreScope {
    let accountID: String?
    let environment: Environment?
    let secrets: AppSecretStore
    let profileStore: ProjectProfileStore
    let activeProjectIDKey: String

    /// Per-account scope rooted at `rootDirectory/accounts/<accountID>/`.
    static func account(_ account: Account,
                        environment: Environment,
                        rootDirectory: URL) -> ProjectStoreScope {
        let accountSecrets = AppSecretStore(
            baseDirectory: rootDirectory
                .appendingPathComponent(AccountStore.accountsDirectoryName, isDirectory: true)
                .appendingPathComponent(AccountStore.directoryName(for: account.id), isDirectory: true)
        )
        return ProjectStoreScope(
            accountID: account.id,
            environment: environment,
            secrets: accountSecrets,
            profileStore: ProjectProfileStore(secrets: accountSecrets),
            activeProjectIDKey: "\(ProjectStore.activeProjectIDKey).\(account.id)"
        )
    }

    /// Single-account/shared scope (no account): legacy defaults + secrets.
    static func shared(defaults: UserDefaults, secrets: AppSecretStore) -> ProjectStoreScope {
        ProjectStoreScope(
            accountID: nil,
            environment: nil,
            secrets: secrets,
            profileStore: ProjectProfileStore(defaults: defaults),
            activeProjectIDKey: ProjectStore.activeProjectIDKey
        )
    }
}

/// Project selection + connection for the signed-in user session.
///
/// Two distinct user decisions:
///
/// - **select** (`selectProject`) = "which project am I viewing/configuring".
///   It only loads/applies that project's profile; it never connects and never
///   mints a token for a non-connected project.
/// - **connect** (`connect`) = "the single engine relay connects to this
///   project". It asks the connector CLI to mint or reuse the project's
///   long-lived token, write the engine config, and restart the engine. Exactly
///   ONE project is connected at a time: connecting B flips A to disconnected.
///
/// The engine runs iff there is a connected project. Selecting a non-connected
/// project writes no project-bound config; when nothing is connected the engine
/// is stopped so no connection lingers for a project the user is only viewing.
///
/// This store is only driven when a signed-in account exists; the connector CLI
/// owns tokens, sessions, and engine-config writing.
@MainActor
final class ProjectStore: ObservableObject {

    enum State: Equatable {
        case idle
        case loading
        case loaded
        /// The connector CLI has no valid session for the active account, so
        /// projects cannot be listed even though the app still has an account
        /// in its index. The UI shows a signed-out prompt with a Sign in
        /// action instead of a raw error.
        case signedOut
        case error(String)
    }

    nonisolated static let activeProjectIDKey = "connector.activeProjectId"

    @Published private(set) var projects: [ProjectInfo] = []
    @Published private(set) var activeProjectID: String?
    @Published private(set) var state: State = .idle
    /// The project whose profile is connected, i.e. the one the single engine
    /// relay serves. Persisted via `ProjectProfile.connected`; at most one at a
    /// time. `nil` → no connection (engine should be stopped).
    @Published private(set) var connectedProjectID: String?
    /// org id → display name, resolved from `GET /api/orgs` (best-effort).
    @Published private(set) var organizationNames: [String: String] = [:]
    /// Bumped whenever a stored profile is written (tool enablement, instance
    /// id, all-tools), so views observing the store re-read the computed
    /// profile accessors. The connection flag has its own published property.
    @Published private(set) var profilesRevision = 0

    private let settings: ConnectorSettings
    private let defaults: UserDefaults
    private var profileStore: ProjectProfileStore
    /// Active account environment; `nil` → shared/single-account defaults.
    private var scopedEnvironment: Environment?
    private var scopedAccountID: String?
    /// Persisted selection key (namespaced per account when scoped).
    private var activeProjectIDKey: String
    private let restart: @MainActor () -> Void
    private let stop: @MainActor () -> Void

    // MARK: - Connector CLI seam
    //
    // Project listing, token minting/reuse, and engine-config writing all run
    // through the bundled `memory-connector` CLI. Injectable so tests can
    // supply a scripted runner; `cliConfigPath` is the `--config` path the CLI
    // writes.

    /// CLI wrapper used for `projects list` / `projects use`. Tests assign a
    /// `ConnectorCLI` built with a fake runner.
    var connectorCLI: ConnectorCLI = ConnectorCLI()
    /// `--config` path passed to the connector CLI.
    var cliConfigPath: String = EngineConfigSync.configURL.path

    /// Best-effort hook the app graph installs to re-establish the connector
    /// CLI session after an auth failure (see `AppEnvironment`). It returns
    /// true when the CLI holds a valid session afterwards, in which case the
    /// project load is retried once. Defaults to "no repair available", so
    /// tests that do not install a hook surface `.signedOut` directly.
    var repairConnectorSession: @MainActor () async -> Bool = { false }

    /// Best-effort hook the app graph installs to resolve organisation ids to
    /// display names (via `GET /api/orgs` on the active account). Returns an
    /// `org id → name` map; an empty map (the default) leaves grouping to fall
    /// back to "Other". Never throws, so a resolver failure can never fail the
    /// project load.
    var organizationNameLoader: @MainActor () async -> [String: String] = { [:] }

    init(settings: ConnectorSettings,
         defaults: UserDefaults = .standard,
         profileStore: ProjectProfileStore? = nil,
         restart: @escaping @MainActor () -> Void = { EngineManager.shared.restart() },
         stop: @escaping @MainActor () -> Void = { EngineManager.shared.stop() }) {
        self.settings = settings
        self.defaults = defaults
        let store = profileStore ?? ProjectProfileStore(defaults: defaults)
        self.profileStore = store
        self.scopedEnvironment = nil
        self.scopedAccountID = nil
        self.activeProjectIDKey = Self.activeProjectIDKey
        self.restart = restart
        self.stop = stop
        self.activeProjectID = defaults.string(forKey: Self.activeProjectIDKey)
        self.connectedProjectID = store.connectedProjectID()
    }

    /// The server URL backing the active scope (account environment or shared).
    private var effectiveServerURL: String {
        scopedEnvironment?.serverURLString ?? settings.serverURL
    }

    // MARK: - Account scope

    /// Swaps the store to `scope` (an account or the shared scope).
    ///
    /// The engine is stopped and in-memory project/connection state is dropped
    /// first; the previous scope's persisted profiles/tokens/selection remain
    /// on disk untouched. The new scope's own persisted selection + connected
    /// project are then loaded. No engine start happens here — a connection is
    /// only materialised by `connect`/`selectProject` for the active account.
    func applyScope(_ scope: ProjectStoreScope) {
        connectedProjectID = nil
        stop()
        projects = []
        organizationNames = [:]
        state = .idle
        activeProjectID = nil

        scopedAccountID = scope.accountID
        scopedEnvironment = scope.environment
        profileStore = scope.profileStore
        activeProjectIDKey = scope.activeProjectIDKey
        activeProjectID = defaults.string(forKey: scope.activeProjectIDKey)
        connectedProjectID = scope.profileStore.connectedProjectID()
    }

    /// Account id of the active scope, or nil for the shared scope.
    var scopedAccount: String? { scopedAccountID }

    /// Re-materialises the engine config for the active scope's connected
    /// project through the CLI (`projects use` reuses the stored token), so a
    /// scope swap can never leave the shared config file pointing at another
    /// account's project (and thus can never run another account's connection).
    ///
    /// Returns `true` when the CLI wrote a valid config (the engine may run).
    /// Returns `false` — and stays stopped — when nothing is connected or the
    /// CLI call fails (e.g. the account needs re-auth). In the latter case only
    /// the in-memory connection is suspended; the persisted `connected` intent
    /// survives so a later reconnect restores it.
    @discardableResult
    func reassertConnection() async -> Bool {
        guard let id = connectedProjectID else {
            stop()
            return false
        }
        let profile = resolvedProfile(for: id)
        do {
            try await runProjectsUse(projectID: id, profile: profile)
            return true
        } catch {
            connectedProjectID = nil
            stop()
            if await Self.isAuthFailure(error, cli: connectorCLI,
                                        serverURL: effectiveServerURL,
                                        configPath: cliConfigPath) {
                state = .signedOut
            } else {
                state = .error(error.localizedDescription)
            }
            return false
        }
    }

    private func persistActiveProjectID() {
        if let activeProjectID {
            defaults.set(activeProjectID, forKey: activeProjectIDKey)
        } else {
            defaults.removeObject(forKey: activeProjectIDKey)
        }
    }

    /// Name for the active project, when known from the loaded list.
    var activeProjectName: String? {
        guard let activeProjectID else { return nil }
        return projects.first { $0.id == activeProjectID }?.name
    }

    /// True when a project is selected (per-project profile editing applies).
    var hasActiveProject: Bool { activeProjectID != nil }

    /// The active project's stored profile, when one exists.
    var activeProfile: ProjectProfile? {
        guard let activeProjectID else { return nil }
        return profileStore.profile(for: activeProjectID)
    }

    /// Effective disabled-tool set for the active project; shared values when
    /// no project is active or none has been stored yet.
    var activeDisabledTools: Set<String> {
        guard let profile = activeProfile else { return settings.disabledTools }
        return Set(profile.disabledTools)
    }

    /// Effective connector instance id for the active project; falls back to
    /// the shared `ConnectorSettings.instanceID`.
    var activeInstanceID: String {
        activeProfile?.instanceID ?? settings.instanceID
    }

    /// True when some project is connected (engine should be running).
    var hasConnectedProject: Bool { connectedProjectID != nil }

    /// True when `projectID` is the connected project.
    func isConnected(_ projectID: String) -> Bool {
        connectedProjectID == projectID
    }

    // MARK: - Loading

    /// Loads the user's projects from the connector CLI
    /// (`projects list --json`). On success, keeps the persisted selection if
    /// it still exists, otherwise selects the first project; then resolves
    /// organisation names best-effort so the switcher can group by real
    /// organisation. On failure the existing selection is preserved.
    ///
    /// The CLI's project rows carry an organisation id; the `accessToken`
    /// parameter is retained for API compatibility but is no longer needed to
    /// list projects.
    func loadProjects(accessToken: String) async {
        state = .loading
        do {
            try await fetchProjects()
        } catch {
            await handleLoadFailure(error)
        }
    }

    /// Runs `projects list` and adopts the result (keeps the persisted
    /// selection when it still exists, otherwise selects the first project),
    /// then resolves organisation names best-effort.
    private func fetchProjects() async throws {
        let rows = try await connectorCLI.projectsList(serverURL: effectiveServerURL,
                                                       configPath: cliConfigPath)
        let loaded = rows.map { ProjectInfo(id: $0.id, name: $0.name, orgID: $0.orgId) }
        projects = loaded
        if let current = activeProjectID, loaded.contains(where: { $0.id == current }) {
            // keep
        } else {
            activeProjectID = loaded.first?.id
            persistActiveProjectID()
        }
        organizationNames = [:]
        state = .loaded
        await resolveOrganizationNames(for: loaded)
    }

    /// Resolves display names for the organisations the loaded projects
    /// reference, via the injected `organizationNameLoader`. Best-effort: the
    /// loader never throws and unresolvable ids simply stay unnamed (grouped
    /// under "Other"). Only ids a loaded project references are registered.
    private func resolveOrganizationNames(for projects: [ProjectInfo]) async {
        let referenced = Set(projects.compactMap(\.orgID))
        guard !referenced.isEmpty else { return }
        let names = await organizationNameLoader()
        for (orgID, name) in names where referenced.contains(orgID) {
            registerOrganizationName(name, forOrgID: orgID)
        }
    }

    /// Classifies a failed project load. An auth/session failure becomes
    /// `.signedOut` (after one best-effort session repair + retry); anything
    /// else stays a `.error`, as before.
    private func handleLoadFailure(_ error: Error) async {
        guard await Self.isAuthFailure(error, cli: connectorCLI,
                                       serverURL: effectiveServerURL,
                                       configPath: cliConfigPath) else {
            state = .error(error.localizedDescription)
            return
        }

        // Auth failure: the app may still think it is signed in. Give the graph
        // one chance to re-import the session into the connector CLI, then
        // retry the list exactly once.
        if await repairConnectorSession() {
            do {
                try await fetchProjects()
                return
            } catch {
                if !(await Self.isAuthFailure(error, cli: connectorCLI,
                                              serverURL: effectiveServerURL,
                                              configPath: cliConfigPath)) {
                    state = .error(error.localizedDescription)
                    return
                }
            }
        }
        state = .signedOut
    }

    /// True when `error` is an auth/session failure: either the connector CLI
    /// reports no valid session, or the failure text is one of the known
    /// not-signed-in / rejected-token signals. The CLI status is the reliable
    /// signal; the text match is the fallback when the status cannot be read
    /// (e.g. the bundled binary is missing).
    static func isAuthFailure(_ error: Error,
                              cli: ConnectorCLI,
                              serverURL: String,
                              configPath: String) async -> Bool {
        if let status = try? await cli.authStatus(serverURL: serverURL, configPath: configPath),
           !status.signedIn {
            return true
        }
        return errorLooksLikeAuthFailure(error)
    }

    /// Text fallback for the auth-failure classification.
    static func errorLooksLikeAuthFailure(_ error: Error) -> Bool {
        let text = ((error as? ConnectorCLIError)?.errorDescription ?? error.localizedDescription)
            .lowercased()
        return text.contains("not signed in")
            || text.contains("invalid_token")
            || text.contains("unauthorized")
            || text.contains("401")
    }

    // MARK: - Organisation grouping

    /// A dropdown section: one organisation (nil = unknown/"Other").
    struct ProjectGroup: Identifiable, Equatable {
        let id: String
        let organization: String?
        let projects: [ProjectInfo]

        /// Guaranteed non-empty menu header. Unknown or blank organisation
        /// names fall back to "Other", so EVERY project row lives under a
        /// `Section` header and renders uniformly indented.
        var sectionTitle: String {
            let trimmed = (organization ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            return trimmed.isEmpty ? "Other" : trimmed
        }
    }

    /// Organisation display name for a project, when known.
    func organizationName(for project: ProjectInfo) -> String? {
        guard let orgID = project.orgID else { return nil }
        if let name = organizationNames[orgID], !name.isEmpty { return name }
        return nil
    }

    /// Records an organisation id → name mapping resolved elsewhere (e.g. the
    /// dashboard's `GET /api/orgs` lookup) so project grouping can use it.
    /// Ignores blank ids/names and unchanged values.
    func registerOrganizationName(_ name: String, forOrgID orgID: String) {
        let trimmedID = orgID.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedID.isEmpty, !trimmedName.isEmpty else { return }
        guard organizationNames[trimmedID] != trimmedName else { return }
        organizationNames[trimmedID] = trimmedName
    }

    /// Projects grouped by organisation name, named groups sorted
    /// alphabetically, unknown-org projects in a final `nil` group (only when
    /// non-empty). Deterministic (projects sorted by name within a group).
    var projectsByOrganization: [ProjectGroup] {
        let grouped = Dictionary(grouping: projects) { organizationName(for: $0) }
        var groups: [ProjectGroup] = grouped
            .filter { $0.key != nil }
            .sorted { ($0.key ?? "") < ($1.key ?? "") }
            .map { name, items in
                ProjectGroup(id: name ?? "", organization: name,
                             projects: items.sorted { ($0.name ?? "") < ($1.name ?? "") })
            }
        if let unknown = grouped[nil], !unknown.isEmpty {
            groups.append(ProjectGroup(id: "__unknown__", organization: nil,
                                       projects: unknown.sorted { ($0.name ?? "") < ($1.name ?? "") }))
        }
        return groups
    }

    // MARK: - Selection (view / configure — never connects)

    /// Brings a project into focus for viewing/configuring. It never connects
    /// and never mints a token for a non-connected project.
    ///
    /// - If `id` IS the connected project: refresh that project's engine config
    ///   through the CLI (token reuse + rewrite) and restart, so profile edits
    ///   take effect.
    /// - Otherwise: only apply/seed the profile for editing; no project-bound
    ///   config is written. If nothing is connected the engine is stopped, so
    ///   no connection lingers for a project the user is merely viewing. If a
    ///   different project is connected, that connection is left untouched.
    func selectProject(_ id: String, accessToken: String) async {
        state = .loading
        let profile = resolvedProfile(for: id)

        if connectedProjectID == id {
            do {
                try await runProjectsUse(projectID: id, profile: profile)
                ensureEngineRunning()
            } catch {
                state = .error(Self.switchErrorMessage(error, projectID: id))
                return
            }
        } else if connectedProjectID == nil {
            // No connected project: the engine must not run for a project the
            // user only selected.
            stop()
        }

        activeProjectID = id
        persistActiveProjectID()
        state = .loaded
    }

    // MARK: - Connect / disconnect (explicit user decision)

    /// Connects the single engine relay to `projectID`: the connector CLI mints
    /// or reuses that project's least-privilege token and writes its engine
    /// config, then the engine restarts and exactly this project is marked
    /// connected (clearing the flag on any other project). Works with zero tools
    /// enabled — the node may report 0 tools while the user tunes them.
    func connect(projectID: String, accessToken: String) async {
        state = .loading
        do {
            // Seed the project's profile (all tools OFF) the first time, then
            // let the CLI mint/reuse the token and write the config BEFORE
            // flipping the connected flag so a failure leaves the previous
            // connection intact.
            let seeded = resolvedProfile(for: projectID)
            try await runProjectsUse(projectID: projectID, profile: seeded)
            // Persist/announce the connection first, then start the engine with
            // THIS project's config (start if stopped, restart if switching).
            profileStore.setConnected(true, for: projectID)
            connectedProjectID = projectID
            activeProjectID = projectID
            persistActiveProjectID()
            ensureEngineRunning()
            state = .loaded
        } catch {
            state = .error(Self.switchErrorMessage(error, projectID: projectID))
        }
    }

    /// Disconnects the current project and stops the engine. Safe to call when
    /// nothing is connected (it still stops the engine).
    func disconnect() {
        if let id = connectedProjectID {
            profileStore.setConnected(false, for: id)
        }
        connectedProjectID = nil
        stop()
        state = .loaded
    }

    /// Enables or disables EVERY `ToolCatalog` tool for `projectID` in one
    /// persisted write. When that project is the connected one the engine
    /// config is rewritten and the engine restarted; otherwise only the stored
    /// profile changes.
    @discardableResult
    func setAllTools(enabled: Bool, projectID: String) async -> ProjectProfile {
        let profile = profileStore.setAllTools(enabled: enabled, for: projectID)
        profilesRevision &+= 1
        await syncEngineIfConnected(projectID: projectID, profile: profile)
        return profile
    }

    /// Actionable switch failure text (surfaced in the toolbar picker's help).
    static func switchErrorMessage(_ error: Error, projectID: String) -> String {
        if let apiError = error as? MemoryAPIError {
            return "Couldn't switch to project \(projectID): \(apiError.errorDescription ?? "\(apiError)")"
        }
        if let cliError = error as? ConnectorCLIError {
            return "Couldn't switch to project \(projectID): \(cliError.errorDescription ?? "\(cliError)")"
        }
        return "Couldn't switch to project \(projectID): \(error.localizedDescription)"
    }

    /// Applies edited connector settings to the active project's profile. The
    /// engine is only touched when that project is the connected one; a
    /// non-connected project's tools are persisted for later. With no active
    /// project it updates the shared defaults, which seed the next project's
    /// profile; the engine stays untouched.
    func saveActiveProfile(disabledTools: Set<String>, instanceID: String) async {
        if let activeProjectID {
            // Preserve the connected flag: editing tools must not disconnect.
            let existing = profileStore.profile(for: activeProjectID)
            let profile = ProjectProfile(disabledTools: disabledTools.sorted(),
                                         instanceID: instanceID,
                                         connected: existing?.connected ?? false)
            profileStore.save(profile, for: activeProjectID)
            profilesRevision &+= 1
            await syncEngineIfConnected(projectID: activeProjectID, profile: profile)
            return
        }

        // No active project: update the shared defaults used to seed profiles.
        settings.instanceID = instanceID
        settings.updateDisabledTools(disabledTools)
    }

    /// Sign-out teardown: stop the engine, clear the connection, and forget the
    /// active project's selection. Connector tokens are owned by the CLI and are
    /// cleaned up on sign-out, so nothing token-related is touched here.
    func stopAndClear() {
        if let connectedProjectID {
            profileStore.setConnected(false, for: connectedProjectID)
        }
        connectedProjectID = nil
        stop()
        activeProjectID = nil
        persistActiveProjectID()
        projects = []
        state = .idle
    }

    // MARK: - Internals

    /// A project's stored profile, or — on first switch — one seeded from the
    /// current shared settings so this project starts from today's values.
    private func resolvedProfile(for id: String) -> ProjectProfile {
        if let existing = profileStore.profile(for: id) {
            return existing
        }
        let seeded = ProjectProfile(disabledTools: Array(settings.disabledTools).sorted(),
                                    instanceID: settings.instanceID)
        profileStore.save(seeded, for: id)
        return seeded
    }

    /// Runs `projects use` for a project's profile. The CLI mints or reuses the
    /// least-privilege token and writes the engine config at `cliConfigPath`.
    private func runProjectsUse(projectID: String, profile: ProjectProfile) async throws {
        _ = try await connectorCLI.projectsUse(project: projectID,
                                               serverURL: effectiveServerURL,
                                               instanceID: profile.instanceID,
                                               disabledTools: profile.disabledTools,
                                               configPath: cliConfigPath)
    }

    /// Rewrites the engine config + restarts iff `projectID` is the connected
    /// project. Non-connected projects are never written to the engine.
    private func syncEngineIfConnected(projectID: String, profile: ProjectProfile) async {
        guard connectedProjectID == projectID else { return }
        do {
            try await runProjectsUse(projectID: projectID, profile: profile)
            ensureEngineRunning()
            state = .loaded
        } catch {
            state = .error(Self.switchErrorMessage(error, projectID: projectID))
        }
    }

    /// Starts the engine when stopped, or restarts it with the freshly written
    /// config. Only ever reached for the connected project (its config is on
    /// disk); the engine must never run for a non-connected project.
    private func ensureEngineRunning() {
        restart()
    }
}
