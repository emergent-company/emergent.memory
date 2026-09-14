import Foundation

/// Loads the agent list from the control plane and tracks the selected agent.
///
/// Lives for the whole app session; the agent picker drives `load()` (the list
/// is read-only — agent configuration happens on web/desktop, not the phone).
/// All state is `@MainActor`; network work runs through a fresh
/// ``ControlPlaneClient`` built from `MemoryConfig()` per call (same pattern as
/// ``MemoryStore``), with stale-result guards so a slow older load can never
/// overwrite a newer one.
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
    /// Name of the currently selected agent (persisted under `alfred.agentName`).
    @Published private(set) var selectedAgentName: String

    /// Bumped on every load; results from loads older than the latest are
    /// discarded, so a slow stale response can't clobber a fresh one.
    private var loadGeneration = 0

    private let defaults: UserDefaults

    /// Starts in the idle state with the persisted (or default) agent name;
    /// the picker triggers the first `load()` when it appears.
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

    /// Persists `agent` as the selected agent and publishes it.
    func select(_ agent: Agent) {
        selectedAgentName = agent.name
        defaults.set(agent.name, forKey: MemoryConfig.agentNameKey)
        TraceLog.log("agent_selected", ["name": agent.name])
    }
}
