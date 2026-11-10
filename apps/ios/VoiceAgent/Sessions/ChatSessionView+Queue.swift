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
        queue.enqueue(text, whileRunning: transport.isStreaming || transport.isRunning)
    }

    /// Send next: one row only. While a turn runs the row is promoted to the
    /// head; when idle the rest stay parked (#1301).
    func sendNextQueued(_ id: String) {
        queue.sendNext(
            id,
            whileRunning: transport.isStreaming || transport.isRunning,
            dispatch: dispatchQueuedText
        )
    }

    /// Releases the head of an armed queue, one row per finished turn.
    func pumpQueue() {
        let sender = makeQueueSender()
        queue.pump(whileRunning: sender.isBlocked, dispatch: sender.dispatch)
    }

    /// Installs the transport's turn-end hook and keeps its target alive for as
    /// long as the view is on screen.
    ///
    /// The transport holds the hook for the view's whole life, so the hook's
    /// target must outlive `start()`: `@State` owns the pump, and the hook only
    /// holds it weakly.
    func attachQueuePump() {
        // Captured by value here rather than through `self`: the hook is stored
        // by the transport and must not reach back into the view.
        let planStore = plan
        queuePump = ChatQueuePump.install(
            on: transport,
            queue: queue,
            sender: makeQueueSender(),
            // A finished turn is when the agent writes or advances its plan, so
            // the plan card refreshes here as well as on a refresh frame (#440).
            alsoOnTurnFinished: { planStore.refresh() }
        )
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

/// The transport's turn-end hook target, owned for the session's lifetime.
///
/// `HTTPChatTransport.onTurnFinished` lives as long as the view does, so the
/// object it calls must outlive `ChatSessionView.start()` — hence a class the
/// view keeps in `@State`, with the hook holding it weakly. It holds stores and
/// the transport, never the view struct, so the transport → hook → pump chain
/// adds no cycle through the view's state storage.
@MainActor
final class ChatQueuePump {
    private let queue: ChatQueueStore
    /// The pump owns the sender: the hook only points here weakly, so a sender
    /// held by a caller's local would die with the call that built it.
    private let sender: ChatQueueSender
    /// A second listener on the same turn-end hook (the plan card, #440).
    private let alsoOnTurnFinished: (() -> Void)?

    private init(
        queue: ChatQueueStore,
        sender: ChatQueueSender,
        alsoOnTurnFinished: (() -> Void)?
    ) {
        self.queue = queue
        self.sender = sender
        self.alsoOnTurnFinished = alsoOnTurnFinished
    }

    /// Builds the session's pump and hooks it to the transport.
    ///
    /// The returned object owns both the sender and the queue and must outlive
    /// the call; the hook holds it weakly, so it stays alive only while the view
    /// keeps it. `alsoOnTurnFinished` runs after the queue releases its row, for
    /// the other listeners a turn's end has (the plan card).
    static func install(
        on transport: HTTPChatTransport,
        queue: ChatQueueStore,
        sender: ChatQueueSender,
        alsoOnTurnFinished: (() -> Void)? = nil
    ) -> ChatQueuePump {
        let pump = ChatQueuePump(
            queue: queue,
            sender: sender,
            alsoOnTurnFinished: alsoOnTurnFinished
        )
        transport.onTurnFinished = { [weak pump] in
            pump?.releaseOneRow()
            pump?.alsoOnTurnFinished?()
        }
        return pump
    }

    /// One finished turn releases one armed row.
    func releaseOneRow() {
        queue.pump(whileRunning: sender.isBlocked, dispatch: sender.dispatch)
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

    /// Whether a turn — or any run parked on a decision, including one older
    /// than the newest turn — owns the composer. A parked run is not streaming
    /// (the composer is free), but its queue waits until the user answers and
    /// the run ends.
    var isBlocked: Bool {
        transport.isStreaming || transport.isRunning || activity.turns.contains(where: \.isWaitingOnUser)
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
