import Foundation
import Observation

// MARK: - Transport

/// Serves a text turn over the gateway's HTTP chat stream and feeds the same
/// transcript/activity/A2UI stores the LiveKit voice path feeds.
///
/// Nothing here mints a token, joins a room or needs a worker: text works on a
/// project with voice disabled and on a deployment with no LiveKit stack.
///
/// A reply can be stopped: closing the stream alone would leave memory running
/// the agent detached (`agents.DetachedRunContext`), so ``stop()`` cancels the
/// `meta` frame's run through `POST /api/chat/runs/{runId}/cancel`.
@MainActor
@Observable
final class HTTPChatTransport {
    /// True while a turn is streaming: gates a second send and the composer.
    private(set) var isStreaming = false
    /// The last turn-level failure, surfaced at the composer. The stream and
    /// ``stop()`` (in its own file) write it; the view only reads it.
    var errorMessage: String?
    /// The open conversation: the launched one, or the id the `meta` frame
    /// reported for a new chat.
    private(set) var conversationID: String?

    /// Run id from the `meta`/`refresh` frames; ``stop()`` cancels it. Cleared
    /// at the end of the turn and in ``reset()`` so it cannot cancel a later run.
    var liveRunID: String?

    /// Attached-run state from conversation events (`#437`, `#470`):
    /// `isRunning` gates the stop control; `isCancelling`/`showsCancelledNotice`
    /// hold the settled stop UI until the next refresh.
    var isRunning = false
    var isCancelling = false
    var showsCancelledNotice = false

    /// Called once when a new conversation's id is first learned, so the UI can
    /// persist it in ``ActiveSessionStore``.
    var onConversationLearned: ((String) -> Void)?

    /// Called when a turn ends on its own, so the composer releases the queue
    /// parked behind it. Not called for an aborted turn (`stop()`, `reset()`).
    var onTurnFinished: (() -> Void)?

    /// The turn whose reply was cut off mid-stream, set only once text is on
    /// screen. The run continues server-side and is recorded when it ends, so
    /// the view waits for that record and swaps it in
    /// (``ChatSessionView/recoverInterruptedTurn()``) instead of reloading the
    /// same truncated one. Empty text leaves this `nil`: a refused request has
    /// nothing to recover.
    private(set) var interruptedTurn: InterruptedTurn?

    /// The last turn that ended without a reply this client could show, set once
    /// the stream is over. Nothing was ever on screen, so the recorded reply is
    /// the only copy; the view waits for it and loads it
    /// (``ChatSessionView/recoverUnansweredTurn()``), and a newer turn clears
    /// this so one wait cannot write into the next chat (`emergent.memory#445`).
    private(set) var unansweredTurn: UnansweredTurn?

    let config: MemoryConfig
    private let stream: any ChatStreaming
    /// Read by ``stop()`` in its own file, so no second session is built.
    let urlSession: URLSession
    /// The turn in flight; ``stop()`` in its own file cancels it. Written only
    /// here (a finished task is replaced by the next ``send``).
    private(set) var turnTask: Task<Void, Never>?
    /// Live transcript fed to the view (user turn + streamed reply).
    private var liveMessages: [LiveTranscriptMessage] = []
    private var fallbackToolCounter = 0
    /// Resolved agent definition id for the selected agent name (cached).
    var agentDefinitionID: String?
    var agentDefinitionResolved = false

    init(
        config: MemoryConfig = MemoryConfig(),
        stream: (any ChatStreaming)? = nil,
        session: URLSession = .shared
    ) {
        self.config = config
        self.stream = stream ?? HTTPChatStreamClient(config: config, session: session)
        urlSession = session
    }

    // MARK: - Turn lifecycle

    /// Sends `text` and streams the reply into `transcript`/`activity`.
    ///
    /// Returns immediately with the turn's task (so tests, and anything else
    /// that needs to, can await it) while ``reset()`` can cancel it. One turn at
    /// a time — a second send while streaming is dropped.
    ///
    /// A `surfaceAction` sends a click on an A2UI card instead of typed text:
    /// the message is then empty and the server frames the turn.
    @discardableResult
    func send(
        _ text: String,
        surfaceAction: ChatSurfaceAction? = nil,
        conversationID: String?,
        agentName: String,
        transcript: ChatSessionTranscriptStore,
        activity: ChatActivityStore
    ) -> Task<Void, Never>? {
        let message = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !message.isEmpty || surfaceAction != nil, !isStreaming else { return nil }
        if let conversationID, !conversationID.isEmpty {
            self.conversationID = conversationID
        }
        errorMessage = nil
        // A newer turn owns the transcript now: a wait for the previous turn's
        // record must not reload it under this one.
        unansweredTurn = nil
        // The new turn replaces any attached-run state the events stream was
        // showing; its own refresh frames will re-establish it.
        resetAttachedRunState()
        isStreaming = true

        // A surface action has no typed text: the card the user clicked is the
        // visible turn, and the record holds the framed message on reload.
        var anchorMessageID: String?
        if !message.isEmpty {
            let userMessageID = "http-user-\(UUID().uuidString)"
            anchorMessageID = userMessageID
            liveMessages.append(LiveTranscriptMessage(id: userMessageID, role: .user, text: message))
            transcript.syncLive(liveMessages)
            activity.userTurnStarted(anchorMessageID: userMessageID)
        }

        let task = Task { @MainActor [weak self] in
            guard let self else { return }
            await runTurn(
                TurnPlan(
                    message: message,
                    surfaceAction: surfaceAction,
                    anchorMessageID: anchorMessageID,
                    agentName: agentName
                ),
                transcript: transcript,
                activity: activity
            )
        }
        turnTask = task
        return task
    }

    /// Answers a pending approval or question. The gateway resumes the paused
    /// run; the caller reloads the transcript to see the continuation.
    func respond(questionID: String, response: String, message: String) async throws {
        try await SessionLogClient(config: config, session: urlSession).respondQuestion(
            questionID: questionID,
            response: response,
            message: message
        )
    }

    /// Drops the live transcript for a new session.
    func reset() {
        turnTask?.cancel()
        turnTask = nil
        isStreaming = false
        errorMessage = nil
        interruptedTurn = nil
        unansweredTurn = nil
        liveMessages.removeAll()
        conversationID = nil
        liveRunID = nil
        resetAttachedRunState()
    }

    // MARK: - Streaming

    // MARK: Live-copy seams

    /// The live messages the view is rendering. The live-copy helpers in
    /// `HTTPChatTransport+LiveCopy.swift` run in another file and cannot read
    /// the `private` storage directly.
    var liveCopy: [LiveTranscriptMessage] {
        liveMessages
    }

    /// Drops one live message (see `HTTPChatTransport+LiveCopy.swift`).
    func removeLiveMessage(_ id: String) {
        liveMessages.removeAll { $0.id == id }
    }

    /// Clears the turn whose reply was cut off, once its record is swapped in.
    func clearInterruptedTurn() {
        interruptedTurn = nil
    }

    /// Clears the turn that never streamed a reply, once its record is in.
    func clearUnansweredTurn() {
        unansweredTurn = nil
    }

    private func runTurn(
        _ plan: TurnPlan,
        transcript: ChatSessionTranscriptStore,
        activity: ChatActivityStore
    ) async {
        var state = TurnState(
            assistantMessageID: "http-assistant-\(UUID().uuidString)",
            anchorMessageID: plan.anchorMessageID
        )
        var failure: Error?
        // The turn owns the run id only while it streams: once it ends (by
        // `done`, failure, or a stop) a later turn must not inherit it.
        // `ended` is set only where a turn really ends, so an aborted turn never
        // releases the queue parked behind it.
        var ended = false
        defer { settleTurn(ended: ended) }

        do {
            let agentID = await resolveAgentDefinitionID(name: plan.agentName)
            let frames = stream.frames(ChatTurnRequest(
                conversationID: conversationID,
                agentDefinitionID: agentID,
                message: plan.message,
                surfaceAction: plan.surfaceAction
            ))
            for try await payload in frames {
                if Task.isCancelled {
                    break
                }
                guard let event = ChatStreamFrameDecoder.decode(
                    payload,
                    fallbackToolID: { [weak self] in
                        guard let self else { return "tool" }
                        fallbackToolCounter += 1
                        return "http-tool-\(fallbackToolCounter)"
                    }
                ) else { continue }
                applyFrame(event, state: &state, transcript: transcript, activity: activity)
            }
        } catch is CancellationError {
            // Cancelled by ``reset()`` or ``stop()``: the view has left this
            // chat or the user ended the turn on purpose, so there is nothing
            // to settle and nothing to recover. A stopped turn is not an
            // interrupted one, so no `interruptedTurn` is raised here.
            return
        } catch {
            guard !Task.isCancelled else { return }
            failure = error
            let message = ChatTurnFailure.message(for: error)
            errorMessage = message
            // Text on screen means the run got going: it is still going
            // server-side and will be recorded when it ends, so the view can
            // wait for the finished reply keeping what it already shows.
            if !state.text.isEmpty {
                interruptedTurn = InterruptedTurn(
                    liveMessageID: state.assistantMessageID,
                    partialText: state.text,
                    // Remembered so settling this drop does not clear a banner a
                    // later turn raised over the same slot.
                    failureMessage: message
                )
            }
        }
        guard !Task.isCancelled else { return }
        finishTurn(state: state, failure: failure, activity: activity)
        ended = true
    }

    /// Clears the turn's streaming state, then tells the queue a turn ended.
    ///
    /// The flags clear first: a pump that starts the next turn must see an idle
    /// transport, and an aborted turn (`ended == false`) must not release the
    /// rows parked behind it.
    private func settleTurn(ended: Bool) {
        isStreaming = false
        liveRunID = nil
        guard ended else { return }
        onTurnFinished?()
    }

    /// Ends a turn whatever the stream did.
    ///
    /// A reply that started is finished, unless the stream stopped without
    /// saying the turn was over: the server always closes a turn with `done`, so
    /// a missing one means this client stopped receiving before the run ended.
    ///
    /// A turn that never produced a token is closed too, and handed to the view
    /// as ``unansweredTurn`` so it can load the finished answer instead of
    /// waiting for a stream that is gone (`emergent.memory#445`).
    private func finishTurn(
        state: TurnState,
        failure: Error?,
        activity: ChatActivityStore
    ) {
        guard !state.started else {
            if !state.completed, interruptedTurn == nil {
                errorMessage = ChatTurnFailure.cutShort
                interruptedTurn = .cutShort(
                    liveMessageID: state.assistantMessageID,
                    partialText: state.text
                )
            }
            activity.markReplyFinished()
            return
        }
        activity.settleWithoutReply()

        // A run that failed is not coming back, and one whose request was
        // refused (or never left this device) never started: in both cases there
        // is nothing to wait for.
        guard !state.serverFailed,
              ChatTurnFailure.isWorthWaiting(for: failure, sawFrame: state.sawFrame),
              let anchorMessageID = state.anchorMessageID
        else { return }
        // A turn that showed its answer in another form (a card, a tool run) is
        // finished, so only an interrupted one is worth waiting on.
        guard !state.completed || !state.sawActivity else { return }

        let failureMessage = errorMessage ?? ChatTurnFailure.noReply
        errorMessage = failureMessage
        unansweredTurn = UnansweredTurn(
            assistantMessageID: state.assistantMessageID,
            anchorMessageID: anchorMessageID,
            failureMessage: failureMessage
        )
    }

    func apply(
        _ event: ChatStreamEvent,
        state: inout TurnState,
        transcript: ChatSessionTranscriptStore,
        activity: ChatActivityStore
    ) {
        state.sawFrame = true
        switch event {
        case let .meta(learnedConversationID, runID):
            // Keep the run id: it is the only handle on the server-side run,
            // and a stop needs it to cancel rather than just drop the stream. A
            // frame without one leaves the id this turn already has.
            liveRunID = runID ?? liveRunID
            guard let learnedConversationID, learnedConversationID != conversationID else { return }
            conversationID = learnedConversationID
            onConversationLearned?(learnedConversationID)
        case let .text(delta):
            if !state.started {
                state.started = true
                activity.agentReplyStarted()
            }
            state.text += delta
            upsertAssistant(state, transcript: transcript)
        case let .activity(chatEvent):
            state.sawActivity = true
            activity.apply(chatEvent)
        case let .citations(citations):
            attach(citations, state: &state, transcript: transcript)
        case let .failure(message):
            state.completed = true
            state.serverFailed = true
            errorMessage = message
            activity.markReplyFinished()
        case let .refresh(runState): applyRunRefresh(runState)
        case .done:
            state.completed = true
            activity.markReplyFinished()
        case .replay:
            // Handled before the switch (``runTurn``); kept for exhaustiveness.
            break
        }
    }

    /// A turn's citations (sent once, after the answer) join its reply's Sources footer.
    private func attach(_ citations: [ChatCitation], state: inout TurnState, transcript: ChatSessionTranscriptStore) {
        state.citations = citations
        if state.started {
            upsertAssistant(state, transcript: transcript)
        }
    }

    private func upsertAssistant(_ state: TurnState, transcript: ChatSessionTranscriptStore) {
        let message = LiveTranscriptMessage(id: state.assistantMessageID, role: .assistant, text: state.text, citations: state.citations)
        if let index = liveMessages.firstIndex(where: { $0.id == message.id }) {
            liveMessages[index] = message
        } else {
            liveMessages.append(message)
        }
        transcript.syncLive(liveMessages)
    }
}
