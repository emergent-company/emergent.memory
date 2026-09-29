import Foundation

/// Holds the object-browser list state for one agent.
///
/// Mirrors ``AgentStore``'s shape: all state is `@MainActor`, network work runs
/// in `Task`s, and a monotonic generation counter discards results from a
/// superseded load so a slow earlier request can never clobber a newer one.
@MainActor
final class ObjectBrowserStore: ObservableObject {
    /// Load state for the object list.
    enum LoadState: Equatable {
        case idle
        case loading
        case loaded
        case failed(String)
    }

    @Published private(set) var objects: [GraphObject] = []
    @Published private(set) var loadState: LoadState = .idle
    @Published private(set) var nextCursor: String?
    @Published private(set) var isLoadingMore = false
    /// The active provenance filter. Set by the segmented control; ``load``
    /// reads it when fetching, so callers refetch after changing it.
    @Published var provenance: ObjectProvenance = .any

    /// Bumped on every full load; results older than the latest are discarded.
    private var generation = 0
    private let client: any ObjectFetching

    /// The client seam makes the store testable without a network; the default
    /// is the live gateway client.
    init(client: any ObjectFetching = ObjectBrowserClient(config: MemoryConfig())) {
        self.client = client
    }

    /// Whether another page is available.
    var hasMore: Bool {
        nextCursor != nil
    }

    /// Loads the first page for `agentID`, resetting the list. Supersedes any
    /// in-flight load (its result is discarded).
    func load(agentID: String) async {
        generation += 1
        let generation = generation
        loadState = .loading
        objects = []
        nextCursor = nil
        do {
            let page = try await client.listObjects(agentID: agentID, provenance: provenance, cursor: nil)
            guard generation == self.generation else { return }
            objects = page.items
            nextCursor = page.nextCursor
            loadState = .loaded
        } catch {
            guard generation == self.generation else { return }
            Log.net.error("object load failed: \(error.localizedDescription)")
            loadState = .failed(error.localizedDescription)
        }
    }

    /// Appends the next page when one is available. A failure leaves the
    /// already-loaded list untouched (the footer simply stops advancing).
    func loadMore(agentID: String) async {
        guard let cursor = nextCursor, !isLoadingMore else { return }
        isLoadingMore = true
        defer { isLoadingMore = false }
        let generation = generation
        do {
            let page = try await client.listObjects(agentID: agentID, provenance: provenance, cursor: cursor)
            guard generation == self.generation else { return }
            objects.append(contentsOf: page.items)
            nextCursor = page.nextCursor
        } catch {
            guard generation == self.generation else { return }
            Log.net.error("object load-more failed: \(error.localizedDescription)")
        }
    }
}
