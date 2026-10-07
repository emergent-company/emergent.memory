import Foundation

/// Loads the agent list from the control plane and tracks the selected agent.
///
/// Lives for the whole app session; the agent picker drives `load()` (the list
/// is read-only — agent configuration happens on web/desktop, not the phone).
/// All state is `@MainActor`; network work runs through a fresh
/// ``ControlPlaneClient`` built from `MemoryConfig()` per call (the same
/// pattern as ``ObjectBrowserStore``), with stale-result guards so a slow older
/// load can never overwrite a newer one.
///
/// The selected agent is scoped per project: each project remembers its own
/// pick under `alfred.agentName.<projectID>`, falling back to the legacy
/// `alfred.agentName` (and the build default) when a project has no stored pick.
@MainActor
final class AgentStore: ObservableObject {
    /// Load state for the agent list.
    enum LoadState: Equatable {
        case idle
        case loading
        case loaded
        case failed(String)
    }

    @Published private(set) var agents: [Agent] = []
    @Published private(set) var loadState: LoadState = .idle
    /// Name of the currently selected agent (persisted under the active
    /// project's key, see `agentNameKey`).
    @Published private(set) var selectedAgentName: String

    /// Bumped on every load; results from loads older than the latest are
    /// discarded, so a slow stale response can't clobber a fresh one.
    private var loadGeneration = 0

    private let defaults: UserDefaults
    /// Project whose agent selection is currently loaded (nil = legacy default).
    private var activeProjectID: String?

    /// Starts in the idle state with the persisted (or default) agent name;
    /// the picker triggers the first `load()` when it appears. `VoiceAgentApp`
    /// calls ``setActiveProject(_:)`` on launch to load the active project's pick.
    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        selectedAgentName = defaults.string(forKey: MemoryConfig.agentNameKey) ?? MemoryConfig.defaultAgentName
    }

    /// The currently selected agent, or nil when it is not in `agents`
    /// (e.g. it was deleted server-side).
    var selectedAgent: Agent? {
        agents.first { $0.name == selectedAgentName }
    }

    /// Fetches `GET /api/agents` into `agents`. Stale-guarded: only the most
    /// recent call may publish its result.
    func load() async {
        loadGeneration += 1
        let generation = loadGeneration
        loadState = .loading
        let client = ControlPlaneClient(config: MemoryConfig())
        do {
            let fetched = try await client.listAgents()
            guard generation == loadGeneration else { return }
            agents = fetched
            Log.agents.info("agents loaded \(fetched.count)")
            loadState = .loaded
        } catch {
            guard generation == loadGeneration else { return }
            Log.agents.error("agents load failed: \(error.localizedDescription)")
            loadState = .failed(error.localizedDescription)
        }
    }

    /// Persists `agent` as the selected agent for the active project and
    /// publishes it.
    func select(_ agent: Agent) {
        selectedAgentName = agent.name
        defaults.set(agent.name, forKey: agentNameKey)
        TraceLog.log("agent_selected", ["name": agent.name])
    }

    /// Loads the agent selection remembered for `id` (or the legacy default
    /// when `id` is nil/empty) and publishes it. Called on launch and whenever
    /// the active project changes.
    func setActiveProject(_ id: String?) {
        let normalized = (id?.isEmpty == false) ? id : nil
        activeProjectID = normalized
        selectedAgentName = defaults.string(forKey: agentNameKey) ?? MemoryConfig.defaultAgentName
    }

    /// The UserDefaults key holding this project's selection: per-project when
    /// a project id is known, otherwise the legacy single-agent key.
    private var agentNameKey: String {
        guard let activeProjectID else { return MemoryConfig.agentNameKey }
        return "\(MemoryConfig.agentNameKey).\(activeProjectID)"
    }
}
