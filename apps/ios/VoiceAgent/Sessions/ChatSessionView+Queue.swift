import Foundation

/// The composer queue's wiring (`emergent.memory#438`): when a send becomes a
/// staged row, and when a row becomes the next turn.
///
/// The rules are the web chat's, one to one. A message parked while a turn runs
/// arms the queue and is released when that turn ends, one row per turn; a
/// message staged with the composer idle (⌘+Return) waits for an explicit
/// action, so an unrelated later turn end cannot release it.
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

    /// Starts one turn from the queue. False when the transport refused to start
    /// it (a turn is already streaming), so the pump can put the row back.
    func dispatchQueuedText(_ text: String) -> Bool {
        makeQueueSender().dispatch(text)
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
        let sender = makeQueueSender()
        queue.pump(whileRunning: sender.isBlocked, dispatch: sender.dispatch)
    }

    /// The transport's turn-end hook.
    ///
    /// The transport holds this for the life of the view, so it captures the
    /// sender (which holds stores, never the view struct) rather than the view:
    /// the struct's state storage owns the transport, and capturing it would be
    /// a retain cycle. The conversation id is resolved when the turn ends, not
    /// here, so a `meta` frame that taught us a new id is already reflected.
    func makeQueuePump(_ sender: ChatQueueSender) -> () -> Void {
        let queue = queue
        return { [weak queue, weak sender] in
            guard let queue, let sender else { return }
            queue.pump(whileRunning: sender.isBlocked, dispatch: sender.dispatch)
        }
    }

    /// The sender both the pump and the hook use: one place for "which
    /// conversation, and is a turn in the way".
    func makeQueueSender() -> ChatQueueSender {
        ChatQueueSender(
            transport: transport,
            activity: activity,
            transcript: transcript,
            activeSession: activeSession,
            route: route
        )
    }

    /// Reopening a chat: a turn that ended while this conversation was off
    /// screen still releases the queue parked behind it.
    ///
    /// The app has no run-status field on a conversation, so "its last run has
    /// ended" is read as "no turn is streaming, no live decision is parked, and
    /// the recorded transcript holds no unanswered approval".
    func maybeReleaseQueueOnReopen() {
        let sender = makeQueueSender()
        queue.releaseOnReopen(
            whileRunning: sender.isBlocked,
            hasConversation: openConversationID != nil,
            lastRunEnded: transcript.pendingApprovals.isEmpty,
            dispatch: sender.dispatch
        )
    }

    func editQueuedText(_ id: String, _ text: String) {
        queue.updateText(text, for: id)
    }

    func removeQueued(_ id: String) {
        queue.remove(id)
    }
}

/// Starts a queued turn for one open chat.
///
/// A class rather than a method so the transport's turn-end hook can hold it
/// without holding the view struct. It resolves the conversation at call time:
/// a brand-new chat learns its id mid-turn, and the next queued row belongs to
/// that conversation, not to the `__new__` scope the row was staged under.
@MainActor
final class ChatQueueSender {
    private let transport: HTTPChatTransport
    private let activity: ChatActivityStore
    private let transcript: ChatSessionTranscriptStore
    private let activeSession: ActiveSessionStore
    private let route: ChatRoute

    init(
        transport: HTTPChatTransport,
        activity: ChatActivityStore,
        transcript: ChatSessionTranscriptStore,
        activeSession: ActiveSessionStore,
        route: ChatRoute
    ) {
        self.transport = transport
        self.activity = activity
        self.transcript = transcript
        self.activeSession = activeSession
        self.route = route
    }

    /// Whether a turn (or a run parked on a decision) owns the composer. A run
    /// parked on a decision is not streaming — the composer is free — but its
    /// queue waits until the user answers and the run ends.
    var isBlocked: Bool {
        transport.isStreaming || activity.liveTurn?.isWaitingOnUser == true
    }

    /// Starts one turn; false when the transport refused (one is streaming).
    func dispatch(_ text: String) -> Bool {
        transport.send(
            text,
            conversationID: conversationID,
            agentName: route.agentName,
            transcript: transcript,
            activity: activity
        ) != nil
    }

    private var conversationID: String? {
        route.conversationID ?? transport.conversationID ?? activeSession.liveConversationID
    }
}
