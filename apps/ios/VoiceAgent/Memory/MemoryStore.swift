import Foundation

/// Holds the memory capability and memory-list state for the currently
/// selected agent.
///
/// Lives for the whole app session; `AgentSessionRoot` triggers a capability
/// refresh whenever the selected agent changes. All state is `@MainActor`;
/// network work runs in `Task`s with stale-result guards.
@MainActor
final class MemoryStore: ObservableObject {
    /// Whether the selected agent has memory (drives the entry point).
    enum Capability: Equatable {
        case unknown
        case available
        case unavailable
    }

    /// Load state for the memories list.
    enum LoadState: Equatable {
        case idle
        case loading
        case loaded
        case failed(String)
    }

    @Published private(set) var capability: Capability = .unknown
    @Published private(set) var loadState: LoadState = .idle
    @Published private(set) var memories: [Memory] = []

    /// The agent the current state belongs to. Results from fetches that no
    /// longer match are discarded, so a fast agent switch can't apply a stale
    /// response.
    private var agent = ""

    /// Starts in the unknown/idle state; `AgentSessionRoot` triggers the first
    /// capability check when the UI appears.
    init() {}

    /// Preview helper: seeds a loaded list without a network call.
    init(previewMemories: [Memory]) {
        memories = previewMemories
        loadState = .loaded
        capability = previewMemories.isEmpty ? .unavailable : .available
    }

    /// Re-checks whether `agent` has memory and clears the stale list state.
    /// Failures (unknown agent, auth, service down) resolve to `.unavailable`.
    func refreshCapability(agent: String) {
        self.agent = agent
        capability = .unknown
        loadState = .idle
        memories = []
        let client = AgentInfoClient(config: MemoryConfig())
        Task { @MainActor [weak self] in
            guard let self, self.agent == agent else { return }
            do {
                let hasMemory = try await client.fetchCapability(agent: agent)
                guard self.agent == agent else { return }
                capability = hasMemory ? .available : .unavailable
            } catch {
                guard self.agent == agent else { return }
                capability = .unavailable
            }
        }
    }

    /// Loads all memories for `agent` (no server-side query; `MemoriesView`
    /// filters the fetched list client-side from the search text).
    func loadMemories(agent: String) {
        self.agent = agent
        loadState = .loading
        let client = AgentInfoClient(config: MemoryConfig())
        Task { @MainActor [weak self] in
            guard let self, self.agent == agent else { return }
            do {
                let fetched = try await client.fetchMemories(agent: agent, query: nil)
                guard self.agent == agent else { return }
                memories = fetched
                loadState = .loaded
            } catch {
                guard self.agent == agent else { return }
                loadState = .failed(error.localizedDescription)
            }
        }
    }
}
