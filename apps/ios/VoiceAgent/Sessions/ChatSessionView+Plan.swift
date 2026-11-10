import Foundation

/// The plan card's wiring (`emergent.memory#440`): which conversation the plan
/// belongs to, and the two refreshes the card does not trigger itself.
///
/// Opening the chat is the card's own `task` (it re-runs when a new chat learns
/// its id). Everything after that is an event the view already sees: a
/// conversation `refresh` frame, and the end of a turn — the two moments the
/// agent's plan can have changed.
extension ChatSessionView {
    /// Re-reads the open conversation's plan.
    ///
    /// The id is resolved now, not when the card first appeared: a brand-new
    /// chat has none until the stream's `meta` frame reports it, and its first
    /// plan is written during that same turn.
    func refreshPlan() {
        let conversationID = openConversationID
        Task { await plan.load(conversationID: conversationID) }
    }
}
