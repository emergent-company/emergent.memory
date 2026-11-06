import Foundation
@testable import Memory
import Testing

// MARK: - Failure handling

/// What a failed turn leaves behind: the interrupted reply it hands the view, the
/// truncated copy it later drops, and the failures that hand over nothing.
///
/// Apart from `HTTPChatTransportTests` so each suite stays under the type-body
/// limit. Nothing here touches the URLProtocol stub — every turn uses
/// `agentName: ""`, which skips the agent lookup — so the two suites share no
/// state and can run in parallel.
@MainActor
struct HTTPChatTransportFailureTests {
    private func makeTransport(payloads: [String], error: Error? = nil) -> HTTPChatTransport {
        HTTPChatTransport(
            config: MemoryConfig(),
            stream: StubChatStream(payloads: payloads, error: error, recorder: nil),
            session: URLSession(configuration: .ephemeral)
        )
    }

    @Test func turnFailureIsReportedAsATurnError() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(payloads: [ChatStreamFixtures.failure])

        let task = try #require(transport.send(
            "hello",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        #expect(transport.errorMessage == "The agent run was interrupted.")
        #expect(transport.isStreaming == false)
    }

    @Test func rejectedCredentialIsNotReportedAsUnreachable() async throws {
        let transport = makeTransport(payloads: [], error: GatewayError.httpStatus(
            401,
            "Chat returned HTTP 401: session_required"
        ))
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()

        let task = try #require(transport.send(
            "hello",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        #expect(transport.errorMessage?.contains("credential was rejected") == true)
    }

    /// A stream that dies after text arrived leaves a run still going
    /// server-side: hand the view what it needs to wait for the record.
    @Test func aReplyCutOffMidStreamIsHandedToTheView() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(
            payloads: [ChatStreamFixtures.meta, ChatStreamFixtures.tokenA],
            error: URLError(.networkConnectionLost)
        )

        let task = try #require(transport.send(
            "hello",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        let interrupted = try #require(transport.interruptedTurn)
        // What did arrive is kept: the user's turn and the partial reply.
        #expect(transcript.liveMessages.map(\.role) == [.user, .assistant])
        #expect(interrupted.liveMessageID == transcript.liveMessages.last?.id)
        #expect(interrupted.partialText == transcript.liveMessages.last?.text)
        #expect(transport.errorMessage != nil)
        #expect(transport.isStreaming == false)
    }

    /// A refusal or a connection that never opened has nothing to wait for.
    @Test func aFailureBeforeAnyTextIsNotRecoverable() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(
            payloads: [],
            error: GatewayError.httpStatus(401, "Chat returned HTTP 401: session_required")
        )

        let task = try #require(transport.send(
            "hello",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        #expect(transport.interruptedTurn == nil)
        #expect(transport.errorMessage != nil)
    }

    /// The record renders on its own: the partial has to go, or it stacks.
    @Test func theTruncatedCopyIsDroppedOnceTheRecordCoversIt() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(
            payloads: [ChatStreamFixtures.meta, ChatStreamFixtures.tokenA],
            error: URLError(.networkConnectionLost)
        )

        let task = try #require(transport.send(
            "hello",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        // The drop reports itself at the composer while it is the last word.
        #expect(transport.errorMessage != nil)

        transport.discardInterruptedLiveCopy(in: transcript)

        #expect(transport.interruptedTurn == nil)
        // The record supersedes the partial, so the banner goes with the copy.
        #expect(transport.errorMessage == nil)
        #expect(transcript.liveMessages.contains { $0.role == .assistant } == false)
        // The user's turn stays: only the assistant copy was superseded.
        #expect(transcript.liveMessages.map(\.role) == [.user])
    }

    /// A recovered drop clears only its own banner. The composer's banner slot is
    /// shared by every turn, so a later failure that landed while the wait ran
    /// has to survive the recovery (#213).
    @Test func aRecoveredDropDoesNotClearAnotherTurnsBanner() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = HTTPChatTransport(
            config: MemoryConfig(),
            stream: SequencedChatStream(turns: [
                StubTurn(
                    payloads: [ChatStreamFixtures.meta, ChatStreamFixtures.tokenA],
                    error: URLError(.networkConnectionLost)
                ),
                // The second turn's stream ends on its own failure frame.
                StubTurn(payloads: [#"{"type":"error","error":"Model timed out"}"#]),
            ]),
            session: URLSession(configuration: .ephemeral)
        )

        let first = try #require(transport.send(
            "hello",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await first.value
        #expect(transport.interruptedTurn != nil)

        // The user sends another turn while the first drop's wait is running; it
        // fails, and its failure now owns the banner.
        let second = try #require(transport.send(
            "and again",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await second.value
        #expect(transport.errorMessage == "Model timed out")

        // The wait then finds the first reply's record and settles that drop.
        transport.discardInterruptedLiveCopy(in: transcript)

        #expect(transport.interruptedTurn == nil)
        // The second turn's failure survives: the user must still see why it
        // produced nothing.
        #expect(transport.errorMessage == "Model timed out")
    }

    /// The deliberate other half of the rule: a drop the wait never settles keeps
    /// its banner, because that banner is the only signal the reply was lost.
    @Test func aDropThatNeverRecoversKeepsItsBanner() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(
            payloads: [ChatStreamFixtures.meta, ChatStreamFixtures.tokenA],
            error: URLError(.networkConnectionLost)
        )

        let task = try #require(transport.send(
            "hello",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        let interrupted = try #require(transport.interruptedTurn)
        // The banner belongs to this drop, which is what lets a settlement tell
        // it apart from a later turn's failure.
        #expect(transport.errorMessage == interrupted.failureMessage)
        // Nothing settled it: both the wait's signal and the banner remain.
        #expect(transport.errorMessage != nil)
    }
}

/// One turn as ``SequencedChatStream`` serves it.
struct StubTurn {
    var payloads: [String] = []
    var error: Error?
}

/// Serves one turn per `frames` call, so two sends can run through one transport —
/// the composer banner slot they share is the subject of one test here.
struct SequencedChatStream: ChatStreaming {
    let turns: [StubTurn]
    private let served = ServedTurns()

    func frames(_: ChatTurnRequest) -> AsyncThrowingStream<String, Error> {
        let next = served.next(of: turns)
        return AsyncThrowingStream { continuation in
            for payload in next.payloads {
                continuation.yield(payload)
            }
            continuation.finish(throwing: next.error)
        }
    }
}

/// Hands ``SequencedChatStream`` its turns in order (written from `frames`).
final class ServedTurns: @unchecked Sendable {
    private var index = 0

    func next(of turns: [StubTurn]) -> StubTurn {
        defer { index += 1 }
        return turns[min(index, turns.count - 1)]
    }
}
