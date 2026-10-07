import Foundation

/// Holds the session-detail timeline state for one room.
///
/// The detail view renders a missing session as an empty timeline rather than
/// a hard error, so a fetch failure does not map to ``LoadState/failed``:
/// instead the store records the failure text in ``failureMessage`` and lands
/// in ``LoadState/loaded`` with an empty record list, which the view derives
/// emptiness from.
@MainActor
final class SessionDetailStore: ObservableObject {
    @Published private(set) var records: [SessionRecord] = []
    @Published private(set) var loadState: LoadState = .idle
    /// Non-nil when the timeline fetch failed (unknown session, unreachable
    /// service); the detail view shows it as a soft empty state, not an error.
    @Published private(set) var failureMessage: String?

    private let client: any SessionLogFetching

    /// The client seam makes the store testable without a network; the default
    /// is the live session-log client.
    init(client: any SessionLogFetching = SessionLogClient(config: MemoryConfig())) {
        self.client = client
    }

    /// Fetches `GET /api/session?room=...` into `records`.
    func load(room: String) async {
        loadState = .loading
        Log.net.info("session timeline room=\(room)")
        do {
            let fetched = try await client.timeline(room: room)
            records = fetched
            Log.net.info("session timeline ok room=\(room) records=\(fetched.count)")
            failureMessage = nil
            loadState = .loaded
        } catch {
            // Unknown session → empty timeline, not an error page.
            records = []
            failureMessage = error.localizedDescription
            Log.net.error("session timeline failed room=\(room): \(error.localizedDescription)")
            loadState = .loaded
        }
    }
}
