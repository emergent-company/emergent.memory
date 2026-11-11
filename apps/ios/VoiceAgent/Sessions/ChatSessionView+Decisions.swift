import Foundation
import SwiftUI

/// The decisions a turn can pause on, and the text turn that starts a chat.
///
/// Split out of `ChatSessionView` to keep both files readable. These are the
/// members that need the view's transport and transcript state, so that state is
/// `internal` rather than `private`.
extension ChatSessionView {
    // MARK: - Decisions

    func decideApproval(_ request: ApprovalRequest, _ decision: ApprovalDecision) {
        guard !request.isAnswered else { return }
        activity.submitApproval(questionId: request.questionId, decision: decision)
        timeline.submitApproval(questionId: request.questionId, decision: decision)
        if activeSession.mode == .text {
            let response: String
            let message: String
            switch decision {
            case .approve:
                response = "approve"
                message = ""
            case let .reject(reason):
                response = "reject"
                message = reason ?? ""
            }
            respondOverHTTP(questionId: request.questionId, response: response, message: message)
            return
        }
        Task { await controller.sendDecision(chatDecision(for: decision, questionId: request.questionId)) }
    }

    /// The open conversation: the route's, the id the HTTP `meta` frame taught
    /// us, or the worker's reported id.
    var openConversationID: String? {
        route.conversationID ?? transport.conversationID ?? activeSession.liveConversationID
    }

    /// Answers a live card over HTTP (text mode): the gateway resumes the paused
    /// run in the background, so the transcript is polled for the continuation.
    func respondOverHTTP(questionId: String, response: String, message: String) {
        guard let conversationID = openConversationID else { return }
        // The ownership fingerprint is taken now, before the decision's traffic
        // can replace the transcript on screen (#208).
        let scope = TranscriptWaitScope(conversationID: conversationID, baseline: transcript.lastRecordCount)
        Task {
            let client = SessionLogClient(config: MemoryConfig())
            // Read the parked record count *before* answering: the resumed run is
            // recorded only after the decision. A fresh text chat never loaded
            // its history, so its own count is zero while the server already
            // holds the parked exchange (#228); measuring against that zero
            // accepted the parked transcript. One read here is the request the
            // wait makes anyway; a failed read leaves the count unknown and the
            // wait falls back to the transcript's baseline.
            let parkedDetail = try? await client.detail(room: conversationID)
            let parkedRecordCount = parkedDetail?.records.count
            do {
                try await transport.respond(questionID: questionId, response: response, message: message)
                await pollResumedHistory(client: client, scope: scope, parkedRecordCount: parkedRecordCount)
            } catch {
                transcript.historyLoadFailed(error.localizedDescription)
                // The banner reads the timeline's error now, so a decision whose
                // follow-up read failed says so where the reader looks.
                timeline.historyLoadFailed(error.localizedDescription)
            }
        }
    }

    /// Recorded approvals belong to a run that is not attached to this room's
    /// worker, so they are answered through the gateway's question endpoint.
    /// Memory resumes the run in the background; the transcript is polled for
    /// the continuation because it does not stream back over LiveKit.
    func decideHistoryApproval(_ request: ApprovalRequest, _ decision: ApprovalDecision) {
        guard let conversationID = openConversationID else {
            return
        }
        let client = SessionLogClient(config: MemoryConfig())
        let scope = TranscriptWaitScope(conversationID: conversationID, baseline: transcript.lastRecordCount)
        transcript.markApprovalAnswered(questionId: request.questionId)
        timeline.submitApproval(questionId: request.questionId, decision: decision)
        Task {
            // The parked record count, read before the decision for the same
            // reason as `respondOverHTTP` (#228).
            let parkedDetail = try? await client.detail(room: conversationID)
            let parkedRecordCount = parkedDetail?.records.count
            do {
                let response: String
                let message: String
                switch decision {
                case .approve:
                    response = "approve"
                    message = ""
                case let .reject(reason):
                    response = "reject"
                    message = reason ?? ""
                }
                try await client.respondQuestion(
                    questionID: request.questionId,
                    response: response,
                    message: message
                )
                await pollResumedHistory(client: client, scope: scope, parkedRecordCount: parkedRecordCount)
            } catch {
                transcript.historyLoadFailed(error.localizedDescription)
                // The banner reads the timeline's error now, so a decision whose
                // follow-up read failed says so where the reader looks.
                timeline.historyLoadFailed(error.localizedDescription)
            }
        }
    }

    /// Waits for the resumed run to append records, then reloads the history.
    ///
    /// The wait carries the conversation it started for and the record count it
    /// was measuring against, and stops as soon as the view no longer matches:
    /// answering a card and switching chats inside the window used to write the
    /// old conversation's records into the new one (`emergent.memory#208`).
    /// `parkedRecordCount` is the server's count before the decision, which the
    /// continuation has to exceed; `nil` falls back to the transcript's count
    /// (`emergent.memory#228`).
    func pollResumedHistory(
        client: any SessionLogFetching,
        scope: TranscriptWaitScope,
        parkedRecordCount: Int?
    ) async {
        let poll = ResumedHistoryPoll(client: client)
        await poll.run(
            scope: scope,
            parkedRecordCount: parkedRecordCount,
            isCurrent: { scopeIsCurrent(scope) },
            apply: { detail in
                transcript.loadHistory(from: detail)
                refreshTimelineFromRecord(conversationID: scope.conversationID)
            }
        )
        // The parked run the user just answered has been recorded: a queue held
        // behind that decision can go now (the wait above ends when the resumed
        // run's records land).
        if scopeIsCurrent(scope) {
            pumpQueue()
        }
    }

    /// Whether a bounded wait that started on `scope` still owns the transcript:
    /// the same conversation is open and its records have not been replaced
    /// since the baseline was read. One predicate for every wait the view runs.
    func scopeIsCurrent(_ scope: TranscriptWaitScope) -> Bool {
        scope.isCurrent(openConversationID: openConversationID, recordCount: transcript.lastRecordCount)
    }

    func answerQuestion(_ request: QuestionRequest, answer: String) {
        guard !request.isAnswered else { return }
        activity.submitQuestion(questionId: request.questionId, answer: answer)
        timeline.submitQuestion(questionId: request.questionId, answer: answer)
        if activeSession.mode == .text {
            respondOverHTTP(questionId: request.questionId, response: answer, message: "")
            return
        }
        Task { await controller.sendDecision(.answer(questionId: request.questionId, value: answer)) }
    }

    func submitSurfaceAction(_ surface: ChatUISurface, _ action: ChatUIAction) {
        if activeSession.mode == .text {
            // The action rides the chat turn: the server frames the message the
            // agent receives, so a click never becomes client-invented prose.
            // A dropped send (a turn is already streaming) leaves the card
            // unsubmitted rather than marking a decision that never left.
            let turn = transport.send(
                "",
                surfaceAction: ChatSurfaceAction(
                    surfaceID: surface.id,
                    componentID: action.componentId,
                    response: action.response
                ),
                conversationID: openConversationID,
                agentName: route.agentName,
                transcript: transcript,
                activity: activity
            )
            guard turn != nil else { return }
            activity.submitSurfaceAction(
                surfaceId: surface.id,
                componentId: action.componentId,
                response: action.response
            )
            return
        }
        activity.submitSurfaceAction(
            surfaceId: surface.id,
            componentId: action.componentId,
            response: action.response
        )
        Task { await controller.sendDecision(.surfaceAction(surfaceId: surface.id, action: action)) }
    }

    func chatDecision(for decision: ApprovalDecision, questionId: String) -> ChatDecision {
        switch decision {
        case .approve:
            .approve(questionId: questionId)
        case let .reject(reason):
            .reject(questionId: questionId, reason: reason)
        }
    }

    // MARK: - Text turns

    /// Sends a text turn over the HTTP chat stream. Never mints a token and
    /// never joins a room.
    ///
    /// A send while a turn is streaming parks the message in the queue, armed
    /// for release when that turn ends (`emergent.memory#438`); the transport
    /// would otherwise drop it.
    func sendText(_ text: String) {
        let message = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !message.isEmpty else { return }
        guard !transport.isStreaming, !transport.isRunning else {
            enqueueText(message)
            return
        }
        _ = dispatchQueuedText(message)
    }

    /// Stops the text turn in flight: the working line and spinners stop now,
    /// and the transport aborts the stream and cancels the run on the gateway.
    /// The transport owns the outcome banner, including the honest "no run id"
    /// case where nothing was cancelled.
    func stopTextTurn() async {
        activity.stopGenerating()
        await transport.stop()
    }
}
