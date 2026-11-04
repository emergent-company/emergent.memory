import Foundation

/// Per-project connector profile: which local MCP tools are OFF for this
/// project, an optional project-specific instance id, whether an engine relay
/// child is connected to this project, and the management port that child binds
/// (when connected). Account/server/OIDC settings stay shared in
/// `ConnectorSettings`; only these knobs vary per project.
struct ProjectProfile: Codable, Equatable, Sendable {
    /// Tool names disabled for this project. A fresh profile defaults to the
    /// whole catalog (all tools OFF); empty means the user enabled all.
    var disabledTools: [String]
    /// nil → fall back to the shared `ConnectorSettings.instanceID`.
    var instanceID: String?
    /// User decision: an engine child is connected to this project. Multiple
    /// profiles may be `true` at once (one child per connected project). Missing
    /// in legacy stored JSON → `false` (existing disabledTools/instanceID are
    /// left untouched).
    var connected: Bool
    /// The loopback management port the connected child binds. `nil` until the
    /// project's child is first launched; a legacy connected profile without a
    /// port is treated as the primary (port `8931`). Missing in legacy JSON →
    /// `nil`.
    var managementPort: Int?

    init(disabledTools: [String],
         instanceID: String? = nil,
         connected: Bool = false,
         managementPort: Int? = nil)
    {
        self.disabledTools = disabledTools
        self.instanceID = instanceID
        self.connected = connected
        self.managementPort = managementPort
    }

    enum CodingKeys: String, CodingKey {
        case disabledTools
        case instanceID
        case connected
        case managementPort
    }

    /// Tolerant decode: profiles written before `connected`/`managementPort`
    /// existed decode as `false`/`nil` without disturbing the other fields.
    /// (`encode` is synthesized.)
    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        disabledTools = try container.decodeIfPresent([String].self, forKey: .disabledTools) ?? []
        instanceID = try container.decodeIfPresent(String.self, forKey: .instanceID)
        connected = try container.decodeIfPresent(Bool.self, forKey: .connected) ?? false
        managementPort = try container.decodeIfPresent(Int.self, forKey: .managementPort)
    }
}

/// Persists per-project profiles.
///
/// The shared/single-account store uses one JSON blob in `UserDefaults` under
/// `connector.profiles`; an account-scoped store uses a 0600 `profiles.json`
/// file inside the account's directory. Decoding is tolerant: a missing or
/// corrupt blob reads as an empty map (never throws, never loses the ability
/// to save).
struct ProjectProfileStore {
    static let key = "connector.profiles"
    /// File name used when the store is backed by an `AppSecretStore`
    /// (per-account directory) instead of a shared `UserDefaults` blob.
    static let defaultFileName = "profiles.json"

    /// Where profiles are persisted. The shared/single-account path keeps the
    /// legacy `UserDefaults` key; per-account scopes use a 0600 file inside the
    /// account directory so profiles never cross accounts.
    private enum Storage {
        case defaults(UserDefaults, key: String)
        case file(AppSecretStore, name: String)
    }

    private let storage: Storage

    /// Shared/profile store backed by `UserDefaults` (single-account path).
    init(defaults: UserDefaults = .standard) {
        storage = .defaults(defaults, key: Self.key)
    }

    /// Account-scoped profile store backed by a file in the account directory.
    init(secrets: AppSecretStore, fileName: String = ProjectProfileStore.defaultFileName) {
        storage = .file(secrets, name: fileName)
    }

    /// Disabled set a brand-new project profile starts with: every catalog
    /// tool. Local MCP tools default OFF, so a newly selected project has all
    /// tools disabled until the user enables some.
    static var defaultDisabledTools: [String] {
        ToolCatalog.tools.map(\.id)
    }

    /// A fresh profile for a project with no stored entry: all catalog tools
    /// OFF. `instanceID` nil falls back to the shared settings at use sites.
    static func newProfile(instanceID: String? = nil) -> ProjectProfile {
        ProjectProfile(disabledTools: defaultDisabledTools, instanceID: instanceID)
    }

    // MARK: - Reads

    func allProjectIDs() -> [String] {
        loadAll().keys.sorted()
    }

    /// The stored profile for a project, or nil when none exists (stored data
    /// is always respected — this is not a "default profile" accessor).
    func profile(for projectID: String) -> ProjectProfile? {
        loadAll()[projectID]
    }

    /// The stored profile, or a fresh all-disabled profile when none exists.
    /// Does not persist the fallback.
    func profileOrDefault(for projectID: String, instanceID: String? = nil) -> ProjectProfile {
        profile(for: projectID) ?? Self.newProfile(instanceID: instanceID)
    }

    /// Every connected project, as a set. Persisted state lives in each
    /// profile's `connected` flag (legacy profiles without it are false). A
    /// legacy single-connected profile reads as a one-element set.
    func connectedProjectIDs() -> Set<String> {
        Set(loadAll().filter(\.value.connected).keys)
    }

    func loadAll() -> [String: ProjectProfile] {
        switch storage {
        case let .defaults(defaults, key):
            guard let data = defaults.data(forKey: key) else { return [:] }
            return (try? JSONDecoder().decode([String: ProjectProfile].self, from: data)) ?? [:]
        case let .file(secrets, name):
            guard let data = secrets.readData(name) else { return [:] }
            return (try? JSONDecoder().decode([String: ProjectProfile].self, from: data)) ?? [:]
        }
    }

    // MARK: - Writes

    func save(_ profile: ProjectProfile, for projectID: String) {
        var all = loadAll()
        all[projectID] = profile
        persist(all)
    }

    func clear(projectID: String) {
        var all = loadAll()
        all.removeValue(forKey: projectID)
        persist(all)
    }

    /// Sets the `connected` flag on `projectID` without touching any other
    /// profile: connections are no longer mutually exclusive, so connecting one
    /// project must not disconnect the others. Creates a fresh all-disabled
    /// profile when the project has none. Persisted state is the profile flag,
    /// so reloading rebuilds the same `connectedProjectIDs`.
    @discardableResult
    func setConnected(_ connected: Bool, for projectID: String) -> ProjectProfile {
        var all = loadAll()
        var target = all[projectID] ?? Self.newProfile()
        target.connected = connected
        all[projectID] = target
        persist(all)
        return target
    }

    /// Persists the loopback management port a connected child binds. Preserves
    /// the other profile fields (connected, disabledTools, instanceID).
    @discardableResult
    func setManagementPort(_ port: Int, for projectID: String) -> ProjectProfile {
        var all = loadAll()
        var target = all[projectID] ?? Self.newProfile()
        target.managementPort = port
        all[projectID] = target
        persist(all)
        return target
    }

    /// Enables or disables EVERY catalog tool for a project in one persisted
    /// write: `enabled == true` clears the disabled set, `false` stores the
    /// whole catalog. Creates the profile when absent. Other fields
    /// (instance id, connected) are preserved.
    @discardableResult
    func setAllTools(enabled: Bool, for projectID: String) -> ProjectProfile {
        var all = loadAll()
        var target = all[projectID] ?? Self.newProfile()
        target.disabledTools = enabled ? [] : Self.defaultDisabledTools
        all[projectID] = target
        persist(all)
        return target
    }

    private func persist(_ all: [String: ProjectProfile]) {
        guard let data = try? JSONEncoder().encode(all) else { return }
        switch storage {
        case let .defaults(defaults, key):
            defaults.set(data, forKey: key)
        case let .file(secrets, name):
            try? secrets.writeData(data, to: name)
        }
    }
}
