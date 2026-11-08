import Foundation
import Observation

// MARK: - Transport

/// Serves a text turn over the gateway's HTTP chat stream and feeds the same
/// transcript/activity/A2UI stores the LiveKit voice path feeds.
///
/// Nothing here mints a token, joins a room or needs a worker: text works on a
/// project with voice disabled and on a deployment with no LiveKit stack.
///
/// A turn cannot be interrupted server-side yet: memory runs the agent on a
/// detached context (`agents.DetachedRunContext`, `chat/handler.go`), so closing
/// the stream leaves the run going. The composer therefore offers no stop
/// control in text mode; cancellation from a device credential needs a
/// device-scoped cancel route on the server and is tracked separately.
@MainActor
@Observable
final class HTTPChatTransport {
    /// True while a turn is streaming: gates a second send and the composer.
    private(set) var isStreaming = false
    /// The last turn-level failure, surfaced at the composer.
    private(set) var errorMessage: String?
    /// The open conversation: the launched one, or the id the `meta` frame
    /// reported for a new chat.
    private(set) var conversationID: String?

    /// Called once when a new conversation's id is first learned, so the UI can
    /// persist it in ``ActiveSessionStore``.
    var onConversationLearned: ((String) -> Void)?

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

    private let config: MemoryConfig
    private let stream: any ChatStreaming
    private let urlSession: URLSession
    private var turnTask: Task<Void, Never>?
    /// Live transcript fed to the view (user turn + streamed reply).
    private var liveMessages: [LiveTranscriptMessage] = []
    private var fallbackToolCounter = 0
    /// Resolved agent definition id for the selected agent name (cached).
    private var agentDefinitionID: String?
    private var agentDefinitionResolved = false

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
    }

    /// Reloads the live transcript into the view (used after the view resets).
    func refreshLiveTranscript(_ transcript: ChatSessionTranscriptStore) {
        transcript.syncLive(liveMessages)
    }

    /// Trades the truncated live copy for the finished reply the record now
    /// holds: the record renders on its own, so keeping the partial would stack
    /// a shorter duplicate underneath it. The banner goes with it — the drop is
    /// no longer the last word on the turn — but only if that banner is still
    /// this drop's: the slot is shared, and a later turn may have failed in the
    /// meantime (`emergent.memory#213`).
    func discardInterruptedLiveCopy(in transcript: ChatSessionTranscriptStore) {
        guard let interrupted = interruptedTurn else { return }
        liveMessages.removeAll { $0.id == interrupted.liveMessageID }
        transcript.syncLive(liveMessages)
        interruptedTurn = nil
        if errorMessage == interrupted.failureMessage {
            errorMessage = nil
        }
    }

    /// Clears the unanswered turn once its recorded reply is on screen, and the
    /// banner with it if that banner is still this turn's.
    ///
    /// Nothing of the turn ever streamed, so the record now supplies both
    /// halves: the live user copy would duplicate the recorded one, and the
    /// empty live turn has nothing left to draw beneath the reply
    /// (`emergent.memory#445`).
    func settleUnansweredTurn(
        in transcript: ChatSessionTranscriptStore,
        activity: ChatActivityStore
    ) {
        guard let unanswered = unansweredTurn else { return }
        unansweredTurn = nil
        liveMessages.removeAll { $0.id == unanswered.anchorMessageID }
        transcript.syncLive(liveMessages)
        activity.discardTurn(anchoredTo: unanswered.anchorMessageID)
        if errorMessage == unanswered.failureMessage {
            errorMessage = nil
        }
    }

    // MARK: - Streaming

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
        defer { isStreaming = false }

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
                apply(event, state: &state, transcript: transcript, activity: activity)
            }
        } catch is CancellationError {
            // Cancelled by ``reset()``: the view has left this chat, so its
            // state is no longer ours to settle.
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
    }

    /// Ends a turn whatever the stream did.
    ///
    /// A reply that started is finished, unless the stream stopped without
    /// saying the turn was over: the server always closes a turn with `done`, so
    /// a missing one means this client stopped receiving before the run ended,
    /// and the reply on screen is a prefix of the recorded one.
    ///
    /// A turn that never produced a token is closed too — otherwise the working
    /// line and the composer's busy gate stay live on a turn that will never move
    /// again — and, when the reply may still be recorded, handed to the view as
    /// ``unansweredTurn`` so it can load the finished answer instead of waiting
    /// for a stream that is gone (`emergent.memory#445`).
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

    private func apply(
        _ event: ChatStreamEvent,
        state: inout TurnState,
        transcript: ChatSessionTranscriptStore,
        activity: ChatActivityStore
    ) {
        state.sawFrame = true
        switch event {
        case let .meta(learnedConversationID, _):
            guard let learnedConversationID, learnedConversationID != conversationID else { return }
            conversationID = learnedConversationID
            onConversationLearned?(learnedConversationID)
        case let .text(delta):
            if !state.started {
                state.started = true
                activity.agentReplyStarted()
            }
            state.text += delta
            upsertAssistant(state.assistantMessageID, text: state.text, transcript: transcript)
        case let .activity(chatEvent):
            state.sawActivity = true
            activity.apply(chatEvent)
        case .citations:
            break
        case let .failure(message):
            state.completed = true
            state.serverFailed = true
            errorMessage = message
            activity.markReplyFinished()
        case .done:
            state.completed = true
            activity.markReplyFinished()
        case let .replay(events):
            for event in events {
                apply(event, state: &state, transcript: transcript, activity: activity)
            }
        }
    }

    private func upsertAssistant(
        _ id: String,
        text: String,
        transcript: ChatSessionTranscriptStore
    ) {
        if let index = liveMessages.firstIndex(where: { $0.id == id }) {
            liveMessages[index] = LiveTranscriptMessage(id: id, role: .assistant, text: text)
        } else {
            liveMessages.append(LiveTranscriptMessage(id: id, role: .assistant, text: text))
        }
        transcript.syncLive(liveMessages)
    }
}

// MARK: - Agent definition

extension HTTPChatTransport {
    /// Resolves the selected agent's definition id, which the chat request
    /// needs to talk to the agent the user picked. Best-effort: when the lookup
    /// fails the request omits it and the server uses the project default.
    private func resolveAgentDefinitionID(name: String) async -> String? {
        if agentDefinitionResolved {
            return agentDefinitionID
        }
        agentDefinitionResolved = true
        guard !name.isEmpty else { return nil }
        do {
            let agents = try await ControlPlaneClient(config: config).listAgents()
            agentDefinitionID = agents.first { $0.name == name }?.id
        } catch {
            agentDefinitionID = nil
        }
        return agentDefinitionID
    }
}
