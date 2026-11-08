import Foundation

/// The composer queue's wiring (`emergent.memory#438`): when a send becomes a
/// staged row, and when a row becomes the next turn.
///
/// The rules are the web chat's, one to one. A message parked while a turn runs
/// arms the queue and is released when that turn ends, one row per turn; a
/// message staged with the composer idle (⌘↩) waits for an explicit action, so
/// an unrelated later turn end cannot release it.
extension ChatSessionView {
    /// Points the queue at the conversation the composer is sending into, and
    /// loads whatever was staged there before.
    ///
    /// The scope is the same id the turn carries: a new chat's id-less scope is
    /// `__new__`, until the stream's `meta` frame reports the id the server
    /// assigned (``ChatQueueStore/learnConversationID(_:)``).
    func scopeQueue() {
        queue.scope(
            projectID: projectStore.activeProjectID ?? "",
            agentName: route.agentName,
            conversationID: openConversationID
        )
    }

    /// Whether a turn (or a run parked on a decision) owns the composer.
    ///
    /// A run parked on a decision is not streaming: the composer is free, but a
    /// queue parked behind that run must not be released until the user answers
    /// and the run ends.
    var isQueueBlocked: Bool {
        transport.isStreaming || activity.liveTurn?.isWaitingOnUser == true
    }

    /// Starts one turn from the queue. False when the transport refused to start
    /// it (a turn is already streaming), so the pump can put the row back.
    func dispatchQueuedText(_ text: String) -> Bool {
        transport.send(
            text,
            conversationID: openConversationID,
            agentName: route.agentName,
            transcript: transcript,
            activity: activity
        ) != nil
    }

    /// Stages a message without sending it. While a turn is running it is armed
    /// for release at that turn's end; when idle it waits for Send next.
    func enqueueText(_ text: String) {
        queue.enqueue(text, whileRunning: transport.isStreaming)
    }

    /// Send next: one row only. While a turn runs the row is promoted to the
    /// head; when idle the rest stay parked (#1301).
    func sendNextQueued(_ id: String) {
        queue.sendNext(id, whileRunning: transport.isStreaming, dispatch: dispatchQueuedText)
    }

    /// Releases the head of an armed queue, one row per finished turn.
    func pumpQueue() {
        queue.pump(whileRunning: isQueueBlocked, dispatch: dispatchQueuedText)
    }

    /// Reopening a chat: a turn that ended while this conversation was off
    /// screen still releases the queue parked behind it.
    ///
    /// The app has no run-status field on a conversation, so "its last run has
    /// ended" is read as "no turn is streaming, no live decision is parked, and
    /// the recorded transcript holds no unanswered approval".
    func maybeReleaseQueueOnReopen() {
        queue.releaseOnReopen(
            whileRunning: isQueueBlocked,
            hasConversation: openConversationID != nil,
            lastRunEnded: transcript.pendingApprovals.isEmpty,
            dispatch: dispatchQueuedText
        )
    }

    func editQueuedText(_ id: String, _ text: String) {
        queue.updateText(text, for: id)
    }

    func removeQueued(_ id: String) {
        queue.remove(id)
    }
}
