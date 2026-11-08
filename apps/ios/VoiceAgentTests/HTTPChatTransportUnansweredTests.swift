import Foundation
@testable import Memory
import Testing

// MARK: - A turn that never showed a reply

/// What a turn leaves behind when the stream ends without a reply this client
/// could show, and what the view does with it (`emergent.memory#445`).
///
/// Split from `HTTPChatTransportFailureTests`, which owns the mid-stream drop:
/// this suite is about the turn that never started a reply at all. Every turn
/// uses `agentName: ""`, which skips the agent lookup, so no URLProtocol stub is
/// involved and the suites share no state.
@MainActor
struct HTTPChatTransportUnansweredTests {
    private func makeTransport(payloads: [String], error: Error? = nil) -> HTTPChatTransport {
        HTTPChatTransport(
            config: MemoryConfig(),
            stream: StubChatStream(payloads: payloads, error: error, recorder: nil),
            session: URLSession(configuration: .ephemeral)
        )
    }

    /// The reported bug: a stream that yields nothing leaves the working line
    /// spinning forever, because nothing ever starts the reply the finishing
    /// guard waits for. The turn has to settle, and the reply has to come from
    /// the record.
    @Test func aStreamThatEndsWithoutATokenSettlesAndIsHandedToTheView() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(payloads: [])

        let task = try #require(transport.send(
            "hello",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        // Settled: no working line, no busy composer.
        #expect(activity.isAwaitingFirstReply == false)
        #expect(activity.isGeneratingReply == false)
        #expect(transport.isStreaming == false)

        // Reported once, and waiting on the record for the answer.
        let unanswered = try #require(transport.unansweredTurn)
        #expect(transport.errorMessage == ChatTurnFailure.noReply)
        #expect(unanswered.failureMessage == ChatTurnFailure.noReply)
        #expect(unanswered.anchorMessageID == transcript.liveMessages.first?.id)
    }

    /// A `done` frame before any token is the same hole: the turn is over, and
    /// whatever it answered with exists only in the record.
    @Test func aDoneFrameBeforeAnyTokenIsAlsoRecoverable() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(payloads: [ChatStreamFixtures.meta, ChatStreamFixtures.done])

        let task = try #require(transport.send(
            "hello",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        #expect(activity.isAwaitingFirstReply == false)
        #expect(transport.unansweredTurn != nil)
    }

    /// A card-only answer is a finished turn, not a dropped one: it settles
    /// without a failure banner and without a pointless wait.
    @Test func aCardOnlyAnswerIsNotReportedAsADroppedTurn() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(payloads: [
            ChatStreamFixtures.meta,
            ChatStreamFixtures.surface,
            ChatStreamFixtures.done,
        ])

        let task = try #require(transport.send(
            "show me",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        #expect(activity.isAwaitingFirstReply == false)
        #expect(activity.isGeneratingReply == false)
        #expect(transport.unansweredTurn == nil)
        #expect(transport.errorMessage == nil)
    }

    /// A stream the server itself reported as failed is over: waiting on the
    /// record would spend the window on a reply that is never coming.
    @Test func aServerReportedFailureDoesNotWaitOnTheRecord() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(payloads: [ChatStreamFixtures.meta, ChatStreamFixtures.failure])

        let task = try #require(transport.send(
            "hello",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        #expect(activity.isAwaitingFirstReply == false)
        #expect(transport.unansweredTurn == nil)
        #expect(transport.errorMessage == "The agent run was interrupted.")
    }

    /// Settling an unanswered turn removes the live half the record now covers,
    /// and clears only this turn's banner.
    @Test func settlingAnUnansweredTurnDropsItsLiveCopyAndBanner() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(payloads: [])

        let task = try #require(transport.send(
            "hello",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value
        #expect(transport.unansweredTurn != nil)
        #expect(activity.liveTurn != nil)

        transport.settleUnansweredTurn(in: transcript, activity: activity)

        #expect(transport.unansweredTurn == nil)
        #expect(transport.errorMessage == nil)
        // The record supplies the user turn now, and the empty live turn is gone.
        #expect(transcript.liveMessages.isEmpty)
        #expect(activity.liveTurn == nil)
    }
}
