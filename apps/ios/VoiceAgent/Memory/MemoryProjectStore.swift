import Foundation

/// Persists every Memory project this device has onboarded and the active one.
///
/// A device credential is project-scoped, so the app holds a list of
/// connections and picks one as active. The active connection is mirrored into
/// the existing `MemoryConfig` keys on every change, which keeps
/// `MemoryConfig()` the single read path for the rest of the app
/// (`ControlPlaneClient`, `MemoryTokenSource`, `ObjectBrowserClient`,
/// `SessionLogClient`) — none of them need to know about projects.
///
/// Every mutation persists the list, mirrors the active connection, and posts
/// `.memoryConfigChanged` when the mirrored values actually changed, so the
/// session controller is rebuilt only when the connection really moved.
@MainActor
final class MemoryProjectStore: ObservableObject {
    /// UserDefaults key holding the JSON-encoded connection list.
    static let projectsKey = "alfred.projects"
    /// UserDefaults key holding the active connection's project id.
    static let activeProjectIDKey = "alfred.activeProjectID"

    @Published private(set) var connections: [MemoryProjectConnection]
    /// Project id of the active connection, or nil when nothing is connected.
    @Published private(set) var activeProjectID: String?

    /// The active connection, resolved from `activeProjectID`.
    var activeConnection: MemoryProjectConnection? {
        guard let activeProjectID else { return nil }
        return connections.first { $0.projectID == activeProjectID }
    }

    private let defaults: UserDefaults
    /// The center `.memoryConfigChanged` is posted to. Injectable so tests can
    /// observe an isolated center instead of `.default`, which the running app
    /// host also observes.
    private let notificationCenter: NotificationCenter

    init(defaults: UserDefaults = .standard, notificationCenter: NotificationCenter = .default) {
        self.defaults = defaults
        self.notificationCenter = notificationCenter

        if defaults.object(forKey: Self.projectsKey) == nil, let migrated = Self.migrateLegacy(defaults: defaults) {
            // First launch after a single-connection install: keep the
            // already-onboarded server rather than appearing unconfigured.
            connections = [migrated]
            activeProjectID = migrated.projectID
            persist()
        } else {
            connections = Self.loadConnections(from: defaults)
            activeProjectID = defaults.string(forKey: Self.activeProjectIDKey)
        }

        // Normalize: a stored id that no longer resolves falls back to the
        // first connection, and a non-empty list always has an active entry.
        if activeProjectID == nil || !connections.contains(where: { $0.projectID == activeProjectID }) {
            activeProjectID = connections.first?.projectID
        }

        // Bring the config mirror in line without posting during init (no
        // observers exist yet, and the controller is built from config anyway).
        _ = mirrorActiveConnection()
    }

    // MARK: - Mutations

    /// Adds `connection`, replacing an existing connection for the same
    /// project (re-onboarding updates in place) or filling in a legacy
    /// placeholder that has no project identity yet. Anything else appends.
    func upsert(_ connection: MemoryProjectConnection) {
        if let index = connections.firstIndex(where: { matches($0, connection) }) {
            connections[index] = connection
            Log.projects.info("updated project \(connection.displayName, privacy: .public)")
        } else {
            connections.append(connection)
            Log.projects.info("added project \(connection.displayName, privacy: .public)")
        }
        persist()

        if activeProjectID == connection.projectID {
            applyActiveConnection()
        } else if activeProjectID == nil {
            // First connection ever: make it active.
            activeProjectID = connection.projectID
            persist()
            applyActiveConnection()
        }
    }

    /// Makes `projectID` active. An unknown id is ignored so a stale selection
    /// can never clear the active connection.
    func setActive(projectID: String?) {
        if let projectID, !connections.contains(where: { $0.projectID == projectID }) {
            return
        }
        guard activeProjectID != projectID else { return }
        activeProjectID = projectID
        persist()
        applyActiveConnection()
        let activeName = activeConnection?.displayName ?? "none"
        Log.projects.info("active project -> \(activeName, privacy: .public)")
    }

    /// Drops `projectID`. When it was active, the first remaining connection
    /// becomes active; when none remain, nothing is active.
    func remove(projectID: String) {
        guard let index = connections.firstIndex(where: { $0.projectID == projectID }) else { return }
        connections.remove(at: index)
        if activeProjectID == projectID {
            activeProjectID = connections.first?.projectID
        }
        persist()
        applyActiveConnection()
        Log.projects.info("removed project \(projectID, privacy: .public)")
    }

    // MARK: - Matching

    private func matches(_ existing: MemoryProjectConnection, _ incoming: MemoryProjectConnection) -> Bool {
        guard !incoming.projectID.isEmpty else { return false }
        // Same project, or the legacy identity-less placeholder this scan fills in.
        return existing.projectID == incoming.projectID || existing.projectID.isEmpty
    }

    // MARK: - Persistence

    private func persist() {
        if let data = try? JSONEncoder().encode(connections) {
            defaults.set(data, forKey: Self.projectsKey)
        }
        if let activeProjectID {
            defaults.set(activeProjectID, forKey: Self.activeProjectIDKey)
        } else {
            defaults.removeObject(forKey: Self.activeProjectIDKey)
        }
    }

    private static func loadConnections(from defaults: UserDefaults) -> [MemoryProjectConnection] {
        guard let data = defaults.data(forKey: projectsKey) else { return [] }
        return (try? JSONDecoder().decode([MemoryProjectConnection].self, from: data)) ?? []
    }

    /// Synthesizes a connection from the legacy single-connection keys when the
    /// project list has never been written but a device credential is present.
    private static func migrateLegacy(defaults: UserDefaults) -> MemoryProjectConnection? {
        let apiKey = defaults.string(forKey: MemoryConfig.apiKeyKey) ?? ""
        guard !apiKey.isEmpty else { return nil }
        return MemoryProjectConnection(
            projectID: "",
            orgID: "",
            projectName: "",
            serverURL: defaults.string(forKey: MemoryConfig.serverURLKey) ?? MemoryConfig.defaultServerURL,
            tokenEndpoint: defaults.string(forKey: MemoryConfig.tokenEndpointKey) ?? MemoryConfig.defaultTokenEndpoint,
            apiBaseURL: defaults.string(forKey: MemoryConfig.apiBaseURLKey) ?? MemoryConfig.defaultAPIBaseURL,
            apiKey: apiKey,
            ttsStrategy: defaults.string(forKey: MemoryConfig.ttsStrategyKey) ?? MemoryConfig.defaultTTSStrategy,
            addedAt: Date()
        )
    }

    // MARK: - Config mirror

    /// Mirrors the active connection into the existing `alfred.*` config keys
    /// (preserving participant identity, exit keywords, timeout and chime from
    /// the current config) and posts `.memoryConfigChanged` when anything
    /// changed.
    private func applyActiveConnection() {
        if mirrorActiveConnection() {
            notificationCenter.post(name: .memoryConfigChanged, object: nil)
        }
    }

    /// Writes the active connection's values into `MemoryConfig` and returns
    /// whether any value actually changed (so same-value mutations stay quiet).
    private func mirrorActiveConnection() -> Bool {
        let current = MemoryConfig(defaults: defaults)
        var updated = current
        if let active = activeConnection {
            updated.serverURL = active.serverURL
            updated.tokenEndpoint = active.tokenEndpoint
            updated.apiBaseURL = active.apiBaseURL
            updated.apiKey = active.apiKey
            updated.ttsStrategy = active.ttsStrategy.isEmpty ? MemoryConfig.defaultTTSStrategy : active.ttsStrategy
            updated.projectID = active.projectID
            updated.orgID = active.orgID
            updated.projectName = active.projectName
        } else {
            updated.projectID = MemoryConfig.defaultProjectID
            updated.orgID = MemoryConfig.defaultOrgID
            updated.projectName = MemoryConfig.defaultProjectName
            // No active project means no credential: without this the removed
            // project's device token stayed in the config mirror and every
            // client kept pointing at a server the user had disconnected from.
            updated.apiKey = ""
        }
        guard updated != current else { return false }
        updated.save(defaults: defaults)
        return true
    }
}
