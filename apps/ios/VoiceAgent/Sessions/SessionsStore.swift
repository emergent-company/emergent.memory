import Foundation

/// Holds the session-list state for the Sessions tab.
///
/// Mirrors ``ObjectBrowserStore``: `@MainActor` state, an injected
/// ``SessionLogFetching`` seam (defaulting to the live ``SessionLogClient``),
/// and a single load path publishing ``LoadState``.
@MainActor
final class SessionsStore: ObservableObject {
    @Published private(set) var sessions: [SessionSummary] = []
    @Published private(set) var loadState: LoadState = .idle
    /// Whether archived conversations are included in the current list.
    @Published private(set) var includeArchived = false

    private let client: any SessionLogFetching
    /// Last filters passed to a load; lifecycle actions reload with them so an
    /// agent-scoped/archived list stays scoped after archive/delete.
    private var lastAgentID: String?

    /// The client seam makes the store testable without a network; the default
    /// is the live session-log client.
    init(client: any SessionLogFetching = SessionLogClient(config: MemoryConfig())) {
        self.client = client
    }

    /// Fetches `GET /api/sessions` into `sessions`, most recently active first.
    func load() async {
        await load(agentID: nil)
    }

    /// Fetches the session list, optionally filtered to one agent definition,
    /// using the currently selected archived-visibility setting.
    func load(agentID: String?) async {
        await load(agentID: agentID, includeArchived: includeArchived)
    }

    /// Fetches the session list with an explicit agent filter and archived
    /// visibility. Both are remembered for lifecycle reloads.
    func load(agentID: String?, includeArchived: Bool) async {
        self.includeArchived = includeArchived
        lastAgentID = agentID
        loadState = .loading
        Log.net.info("listSessions agent=\(agentID ?? "-") archived=\(includeArchived)")
        do {
            let fetched = try await client.listSessions(
                agentID: agentID,
                includeArchived: includeArchived
            )
            sessions = fetched
            Log.net.info("listSessions ok \(fetched.count)")
            loadState = .loaded
        } catch {
            // A cancelled list fetch is not a failure; the view's task re-runs
            // when it appears again.
            guard !Cancellation.isCancellation(error), !Cancellation.isCurrentTaskCancelled else {
                loadState = .idle
                return
            }
            Log.net.error("listSessions failed: \(error.localizedDescription)")
            loadState = .failed(error.localizedDescription)
        }
    }

    /// Changes archived visibility for subsequent loads. Does not itself fetch;
    /// call ``load(agentID:)``/``load(agentID:includeArchived:)`` to refresh.
    func setIncludeArchived(_ includeArchived: Bool) {
        self.includeArchived = includeArchived
    }

    // MARK: - Lifecycle

    /// Archives `id`, then reloads the current (possibly agent-filtered) list.
    /// Returns whether the server accepted the action, so callers can end a
    /// live room for the conversation they just closed.
    @discardableResult
    func archive(id: String) async -> Bool {
        await perform { try await client.archiveConversation(id: id) }
    }

    /// Restores an archived conversation, then reloads.
    @discardableResult
    func unarchive(id: String) async -> Bool {
        await perform { try await client.unarchiveConversation(id: id) }
    }

    /// Deletes `id`, then reloads.
    @discardableResult
    func delete(id: String) async -> Bool {
        await perform { try await client.deleteConversation(id: id) }
    }

    private func perform(_ action: () async throws -> Void) async -> Bool {
        do {
            try await action()
        } catch {
            Log.net.error("session lifecycle failed: \(error.localizedDescription)")
            loadState = .failed(error.localizedDescription)
            return false
        }
        await load(agentID: lastAgentID, includeArchived: includeArchived)
        return true
    }
}
