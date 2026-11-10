import Foundation
import Observation

/// The agent's plan for the open conversation (`emergent.memory#440`).
///
/// The plan is the conversation's ACP session todo list — the same list the web
/// chat renders above its composer. It is decoration, never a source of truth for
/// the transcript: a failed fetch keeps the last plan it showed instead of
/// flashing the card away, and an empty plan hides the card entirely.
///
/// Refreshed from three triggers, all owned by ``ChatSessionView``: opening the
/// chat (the card's own `task`, which also re-runs when a new chat learns its
/// id), a conversation `refresh` frame, and the end of a turn.
@MainActor
@Observable
final class ChatPlanStore {
    private let client: any SessionTodoFetching

    /// The conversation the plan belongs to; nil while a new chat has no id yet.
    private(set) var conversationID: String?
    /// The plan, in the server's order.
    private(set) var todos: [SessionTodo] = []

    /// Invalidates a fetch that finishes after the conversation moved on.
    @ObservationIgnored private var generation = 0

    /// The client seam keeps this testable without a network; the default is the
    /// live session-log client.
    init(client: any SessionTodoFetching = SessionLogClient(config: MemoryConfig())) {
        self.client = client
    }

    /// How many items are done, for the collapsed "Plan · done/total" line.
    var doneCount: Int {
        todos.filter { $0.status == SessionTodoStatus.completed.rawValue }.count
    }

    /// Fetches the plan for `conversationID`, dropping the previous
    /// conversation's plan immediately so one chat never shows another's.
    ///
    /// A nil id (a brand-new chat) clears the plan: the card has nothing to show
    /// until the stream's `meta` frame reports the id.
    func load(conversationID: String?) async {
        guard let conversationID else {
            clear()
            return
        }
        if self.conversationID != conversationID {
            self.conversationID = conversationID
            todos = []
        }
        generation += 1
        let fetchGeneration = generation
        do {
            let fetched = try await client.todos(conversationID: conversationID)
            // A newer load (another conversation, another refresh) superseded
            // this one while it was in flight; its plan belongs to a chat that
            // has moved on.
            guard fetchGeneration == generation else { return }
            todos = fetched.sorted { $0.order < $1.order }
        } catch {
            // Keep the last good plan: a plan card is not worth an error banner,
            // and the next refresh (or turn end) retries.
            guard fetchGeneration == generation else { return }
        }
    }

    /// Re-reads the current conversation's plan. Safe to fire from a synchronous
    /// callback: the request runs after the caller returns, and a newer one wins.
    func refresh() {
        guard let conversationID else { return }
        Task { await load(conversationID: conversationID) }
    }

    /// Drops the plan and its conversation, so the card renders nothing.
    func clear() {
        generation += 1
        conversationID = nil
        todos = []
    }
}
