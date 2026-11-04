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
        let baseline = transcript.lastRecordCount
        Task {
            do {
                try await transport.respond(questionID: questionId, response: response, message: message)
                await pollResumedHistory(
                    client: SessionLogClient(config: MemoryConfig()),
                    conversationID: conversationID,
                    baseline: baseline
                )
            } catch {
                transcript.historyLoadFailed(error.localizedDescription)
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
        let baseline = transcript.lastRecordCount
        transcript.markApprovalAnswered(questionId: request.questionId)
        Task {
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
                await pollResumedHistory(client: client, conversationID: conversationID, baseline: baseline)
            } catch {
                transcript.historyLoadFailed(error.localizedDescription)
            }
        }
    }

    /// Waits for the resumed run to append records, then reloads the history.
    func pollResumedHistory(
        client: SessionLogClient,
        conversationID: String,
        baseline: Int
    ) async {
        for _ in 0 ..< 30 {
            try? await Task.sleep(for: .seconds(1))
            guard let detail = try? await client.detail(room: conversationID) else { continue }
            if detail.records.count > baseline {
                transcript.loadHistory(from: detail)
                return
            }
        }
    }

    func answerQuestion(_ request: QuestionRequest, answer: String) {
        guard !request.isAnswered else { return }
        activity.submitQuestion(questionId: request.questionId, answer: answer)
        if activeSession.mode == .text {
            respondOverHTTP(questionId: request.questionId, response: answer, message: "")
            return
        }
        Task { await controller.sendDecision(.answer(questionId: request.questionId, value: answer)) }
    }

    func submitSurfaceAction(_ surface: ChatUISurface, _ action: ChatUIAction) {
        // A generic A2UI surface action has no HTTP transport yet (product #66),
        // so a text session renders surfaces read-only — the same thing the web
        // chat can do today. Voice keeps the worker round-trip. Approval- and
        // question-shaped cards are answered through the routes above.
        guard activeSession.mode == .voice else { return }
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
    func sendText(_ text: String) {
        transport.send(
            text,
            conversationID: openConversationID,
            agentName: route.agentName,
            transcript: transcript,
            activity: activity
        )
    }
}
