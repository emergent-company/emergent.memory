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

    private let client: any SessionLogFetching

    /// The client seam makes the store testable without a network; the default
    /// is the live session-log client.
    init(client: any SessionLogFetching = SessionLogClient(config: MemoryConfig())) {
        self.client = client
    }

    /// Fetches `GET /api/sessions` into `sessions`, most recently active first.
    func load() async {
        loadState = .loading
        Log.net.info("listSessions")
        do {
            let fetched = try await client.listSessions()
            sessions = fetched
            Log.net.info("listSessions ok \(fetched.count)")
            loadState = .loaded
        } catch {
            Log.net.error("listSessions failed: \(error.localizedDescription)")
            loadState = .failed(error.localizedDescription)
        }
    }
}
