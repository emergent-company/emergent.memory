import Foundation

/// The values one text turn is described by, and the two ways it can end without
/// the live reply it was hoping for.
///
/// All four are small enough to describe here rather than in the middle of the
/// transport's turn machinery, and none of them holds transport state.
extension HTTPChatTransport {
    /// What one turn sends: the text (or card click), the message it anchors to,
    /// and the agent the session is pointed at.
    struct TurnPlan {
        let message: String
        let surfaceAction: ChatSurfaceAction?
        let anchorMessageID: String?
        let agentName: String
    }

    /// What one turn accumulates while its frames stream in.
    struct TurnState {
        let assistantMessageID: String
        /// The user message that opened the turn; `nil` for a surface action,
        /// which has no typed text to anchor to.
        let anchorMessageID: String?
        var text = ""
        var started = false
        /// A decoded frame arrived: the gateway accepted the turn, so a run
        /// exists that may still be recorded.
        var sawFrame = false
        /// Something visible accumulated besides text — a reasoning block, a
        /// tool call, a card. A card-only answer is a finished turn, not a
        /// dropped one.
        var sawActivity = false
        /// The stream said the turn is over (`done`), so the run is no longer
        /// in flight.
        var completed = false
        /// The stream reported the run failed; no reply is coming.
        var serverFailed = false
    }

    /// A reply the client saw cut off mid-stream, and the live copy holding it.
    struct InterruptedTurn: Equatable, Sendable {
        /// Live message carrying the truncated text, dropped once the record
        /// holds the finished reply.
        let liveMessageID: String
        /// What the user already saw, used to recognise the finished reply.
        let partialText: String
        /// The banner this drop raised. The composer's banner slot is shared by
        /// every turn, so settling this drop must not clear a later turn's
        /// failure: `discardInterruptedLiveCopy` only clears the banner when it
        /// is still this one (`emergent.memory#213`).
        let failureMessage: String

        /// The hand-over for a reply the stream stopped delivering before `done`
        /// — no error, no end of turn, just no more frames.
        static func cutShort(liveMessageID: String, partialText: String) -> InterruptedTurn {
            InterruptedTurn(
                liveMessageID: liveMessageID,
                partialText: partialText,
                failureMessage: ChatTurnFailure.cutShort
            )
        }
    }

    /// What a stop asked for and what came of it.
    ///
    /// ``cancelled`` is only ever returned for the gateway's explicit
    /// `{"ok": true}`: a local abort on its own is not a cancellation and must
    /// never be reported as one, because the run keeps going server-side.
    enum StopOutcome: Equatable, Sendable {
        /// The run was cancelled: `POST /api/chat/runs/{id}/cancel` answered
        /// `{"ok": true}`.
        case cancelled
        /// No run id arrived in a `meta` frame, so no request was made and
        /// nothing is claimed about the server-side run.
        case noRunID
        /// The gateway refused or the request failed, with the message to show.
        case failed(String)
    }

    /// A turn that ended without any reply this client could show, and the
    /// message that anchored it.
    ///
    /// No token means nothing streamed into the transcript, so there is no
    /// partial to keep or to recognise: the reply is only ever the recorded one.
    /// The view waits for that record and loads it (``ChatSessionView``), so a
    /// dropped or empty stream no longer leaves the working line spinning until
    /// the chat is reopened (`emergent.memory#445`).
    struct UnansweredTurn: Identifiable, Equatable, Sendable {
        /// The failing turn's reply slot (kept for identity and tests).
        let assistantMessageID: String
        /// The user message that opened the turn; the record covers it.
        let anchorMessageID: String
        /// The banner this failure raised. Shared banner slot, so settling must
        /// only clear this one (`emergent.memory#213`).
        let failureMessage: String

        var id: String {
            assistantMessageID
        }
    }
}
