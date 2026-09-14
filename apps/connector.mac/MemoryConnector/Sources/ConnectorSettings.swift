import Combine
import Foundation

/// Single source of truth for non-secret connection settings.
///
/// Sign-in and connector secrets are owned by the bundled `memory-connector`
/// CLI; the app keeps only non-secret defaults in `UserDefaults` under the
/// `connector.` prefix. Tool enablement stores only the disabled subset here.
@MainActor
final class ConnectorSettings: ObservableObject {

    nonisolated private static let serverURLKey = "connector.serverURL"
    private static let instanceIDKey = "connector.instanceID"
    private static let disabledToolsKey = "connector.disabledTools"
    /// Presence marker: distinguishes "user saved an empty disabled set" from
    /// "never configured" (so import does not clobber an intentional empty set
    /// and only backfills a genuinely unset one).
    private static let disabledToolsInitializedKey = "connector.disabledToolsInitialized"

    private let defaults: UserDefaults

    @Published var serverURL: String
    @Published var instanceID: String

    /// Every catalog tool id. A never-configured install starts with all local
    /// MCP tools OFF, i.e. the disabled set defaults to the whole catalog.
    static var allCatalogToolIDs: Set<String> { Set(ToolCatalog.tools.map(\.id)) }

    @Published private(set) var disabledTools: Set<String>
    /// True when the last `importExistingConfigIfNeeded` pulled values from an
    /// existing engine config file.
    @Published private(set) var importedFromConfig: Bool = false
    /// Bumped on every successful settings write (`clearConfiguration`). The
    /// identity store observes this to reload without reacting to every
    /// keystroke in the connection fields.
    @Published private(set) var configurationRevision = 0

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        serverURL = defaults.string(forKey: Self.serverURLKey) ?? ""
        instanceID = defaults.string(forKey: Self.instanceIDKey) ?? Self.defaultInstanceID()
        disabledTools = Self.loadDisabledTools(defaults: defaults)
    }

    /// Live, nonisolated read of the persisted `serverURL`. The one-time legacy
    /// migration's `serverURLProvider` runs off the main actor, so it cannot
    /// touch the actor-isolated `serverURL` property; this reads the same
    /// (always-persisted) value through thread-safe `UserDefaults`.
    nonisolated static func persistedServerURL() -> String {
        UserDefaults.standard.string(forKey: serverURLKey) ?? ""
    }

    /// Resolves the disabled set with the "all local MCP tools default OFF"
    /// rule, without changing any stored value:
    ///
    /// - a stored `disabledTools` list (with or without the marker) is honored
    ///   verbatim, so existing installs and user-intended empty sets (all ON)
    ///   are never migrated;
    /// - the marker present without a list is also treated as an intentional
    ///   empty set (all ON);
    /// - neither present → never configured → every catalog tool is disabled.
    private static func loadDisabledTools(defaults: UserDefaults) -> Set<String> {
        if let stored = defaults.stringArray(forKey: Self.disabledToolsKey) {
            return Set(stored)
        }
        if defaults.object(forKey: Self.disabledToolsInitializedKey) != nil {
            return []
        }
        return Self.allCatalogToolIDs
    }

    /// Default instance id mirrors the engine's `<hostname>-connector`.
    static func defaultInstanceID() -> String {
        let host = ProcessInfo.processInfo.hostName
        let base = host.hasSuffix(".local") ? String(host.dropLast(".local".count)) : host
        let trimmed = base.isEmpty ? "localhost" : base
        return "\(trimmed)-connector"
    }

    /// Persists non-secret settings.
    func persist() {
        defaults.set(serverURL, forKey: Self.serverURLKey)
        defaults.set(instanceID, forKey: Self.instanceIDKey)
        defaults.set(Array(disabledTools).sorted(), forKey: Self.disabledToolsKey)
        defaults.set(true, forKey: Self.disabledToolsInitializedKey)
    }

    /// Replaces the disabled tool set. The caller then materializes the engine
    /// config through the connected project's profile.
    func updateDisabledTools(_ tools: Set<String>) {
        disabledTools = tools
        persist()
    }

    /// Toggle state for a catalog tool: enabled == NOT in the disabled set.
    func isToolEnabled(_ id: String) -> Bool {
        !disabledTools.contains(id)
    }

    /// Flips one catalog tool on/off and persists the disabled set.
    func setToolEnabled(_ id: String, enabled: Bool) {
        if enabled {
            disabledTools.remove(id)
        } else {
            disabledTools.insert(id)
        }
        persist()
    }

    /// Clears all persisted non-secret settings back to a fresh install (used
    /// by tests and a future "Disconnect" affordance).
    func clearConfiguration() {
        defaults.removeObject(forKey: Self.serverURLKey)
        defaults.removeObject(forKey: Self.instanceIDKey)
        defaults.removeObject(forKey: Self.disabledToolsKey)
        defaults.removeObject(forKey: Self.disabledToolsInitializedKey)
        serverURL = ""
        instanceID = Self.defaultInstanceID()
        // Back to a fresh install: all local MCP tools OFF (keys were removed,
        // so a reload would resolve to the same).
        disabledTools = Self.allCatalogToolIDs
        importedFromConfig = false
        configurationRevision &+= 1
    }

    /// Backfills app state from an existing engine config file (typically
    /// created by the CLI). Per-field, not all-or-nothing: whenever the file
    /// exists, only the missing pieces are adopted —
    ///
    /// - empty saved server URL  → take the file's (and persist);
    /// - empty saved instance id → take the file's (and persist);
    /// - disabled set never saved AND the file declares `disabled_tools` →
    ///   adopt the file's list.
    ///
    /// It never overwrites a non-empty saved server URL, a saved instance id,
    /// or an already-saved disabled set. A file with no `disabled_tools` block
    /// leaves the "all tools OFF" default in place rather than silently
    /// enabling everything. `importedFromConfig` is set when anything was
    /// adopted.
    func importExistingConfigIfNeeded(configURL: URL) {
        guard let parsed = EngineConfigWriter.readDetailed(configURL: configURL) else { return }
        let values = parsed.values

        var persistNeeded = false
        var imported = false

        // "Unset" is decided by UserDefaults PRESENCE, never by the in-memory
        // value: instanceID has a non-empty hostname default, so comparing the
        // property would wrongly look "already saved".
        let savedServerURL = (defaults.string(forKey: Self.serverURLKey) ?? "")
            .trimmingCharacters(in: .whitespaces)
        if savedServerURL.isEmpty, !values.serverURL.isEmpty {
            serverURL = values.serverURL
            persistNeeded = true
            imported = true
        }

        let savedInstanceID = defaults.string(forKey: Self.instanceIDKey) ?? ""
        if savedInstanceID.trimmingCharacters(in: .whitespaces).isEmpty, !values.instanceID.isEmpty {
            instanceID = values.instanceID
            persistNeeded = true
            imported = true
        }

        // "Never saved" is detected by the presence marker, not by emptiness.
        // Only adopt when the file actually declared a disabled_tools block;
        // otherwise keep the all-tools-OFF default instead of re-enabling.
        if defaults.object(forKey: Self.disabledToolsInitializedKey) == nil,
           parsed.hasDisabledTools {
            disabledTools = Set(values.disabledTools)
            persistNeeded = true
            imported = true
        }

        if persistNeeded {
            persist()
        }

        if imported {
            importedFromConfig = true
        }
    }
}
