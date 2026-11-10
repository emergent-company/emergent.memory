import Foundation

/// One top-level Chats row: a conversation plus every sub-chat that descends
/// from it, in server (most-recently-active) order. Children are flattened to
/// their root so a grandchild can never be dropped from the list.
struct SessionThread: Identifiable, Equatable {
    let summary: SessionSummary
    let children: [SessionSummary]

    var id: String {
        summary.id
    }

    var childCount: Int {
        children.count
    }
}

/// Groups the flat `GET /api/sessions` list into parent rows and their
/// sub-chats.
///
/// A conversation whose parent is absent from `sessions` is promoted to a root
/// (the same orphan rule the web rail uses), so a child is never lost.
/// `matches` filters rows: a parent stays visible when any of its children
/// matches, which keeps a filtered-in sub-chat reachable under its parent.
enum SessionThreading {
    static func threads(
        from sessions: [SessionSummary],
        matching matches: (SessionSummary) -> Bool = { _ in true }
    ) -> [SessionThread] {
        let byID = Dictionary(sessions.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        let ids = Set(byID.keys)

        func rootID(of summary: SessionSummary) -> String {
            var current = summary
            var visited: Set<String> = [current.id]
            while let parent = current.parentConversationID,
                  !parent.isEmpty,
                  ids.contains(parent),
                  !visited.contains(parent),
                  let next = byID[parent]
            {
                visited.insert(parent)
                current = next
            }
            return current.id
        }

        var roots: [SessionSummary] = []
        var childrenByRoot: [String: [SessionSummary]] = [:]
        for summary in sessions {
            let root = rootID(of: summary)
            if root == summary.id {
                roots.append(summary)
            } else {
                childrenByRoot[root, default: []].append(summary)
            }
        }

        return roots.compactMap { root in
            let children = (childrenByRoot[root.id] ?? []).filter(matches)
            guard matches(root) || !children.isEmpty else { return nil }
            return SessionThread(summary: root, children: children)
        }
    }
}

/// The outcome of one bulk-archive pass. `failed` holds only the ids whose
/// request failed; every other requested row stayed archived.
struct BulkArchiveResult: Equatable {
    let requested: [String]
    let failed: [String]

    var failedCount: Int {
        failed.count
    }

    var didFail: Bool {
        !failed.isEmpty
    }
}

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
    /// Conversations this store archived but still hides while unarchived-only
    /// lists refresh. A refresh that lands while the archive requests are in
    /// flight (or before the server has caught up) must not resurrect a row the
    /// user just saw disappear. Failed requests remove their id here so only
    /// those rows come back.
    private var locallyArchivedIDs: Set<String> = []

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
            // Keep rows hidden while their archive is settling (or while the
            // server list lags); a reload must never resurrect them.
            sessions = includeArchived
                ? fetched
                : fetched.filter { !locallyArchivedIDs.contains($0.id) }
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
        let succeeded = await perform { try await client.archiveConversation(id: id) }
        if succeeded, !includeArchived {
            locallyArchivedIDs.insert(id)
            sessions.removeAll { $0.id == id }
        }
        return succeeded
    }

    /// Bulk archive: hides every selected row immediately, fires the requests
    /// concurrently, and restores only the rows whose request failed. Returns
    /// the failed ids so the caller can surface one error toast.
    @discardableResult
    func archive(ids: [String]) async -> BulkArchiveResult {
        let requested = Array(Set(ids)).sorted()
        guard !requested.isEmpty else {
            return BulkArchiveResult(requested: [], failed: [])
        }
        let hidesRows = !includeArchived
        if hidesRows {
            locallyArchivedIDs.formUnion(requested)
            sessions.removeAll { requested.contains($0.id) }
        }
        let failed = await withTaskGroup(of: String?.self, returning: [String].self) { group in
            for id in requested {
                group.addTask { [client] in
                    do {
                        try await client.archiveConversation(id: id)
                        return nil
                    } catch {
                        return id
                    }
                }
            }
            var failed: [String] = []
            for await id in group {
                if let id {
                    failed.append(id)
                }
            }
            return failed.sorted()
        }
        if hidesRows {
            // Only the failures come back. The successes stay hidden even if a
            // refresh races the requests or the server list lags.
            locallyArchivedIDs.subtract(failed)
        }
        await load(agentID: lastAgentID, includeArchived: includeArchived)
        return BulkArchiveResult(requested: requested, failed: failed)
    }

    /// Restores an archived conversation, then reloads.
    @discardableResult
    func unarchive(id: String) async -> Bool {
        locallyArchivedIDs.remove(id)
        return await perform { try await client.unarchiveConversation(id: id) }
    }

    /// Deletes `id`, then reloads.
    @discardableResult
    func delete(id: String) async -> Bool {
        locallyArchivedIDs.remove(id)
        return await perform { try await client.deleteConversation(id: id) }
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
