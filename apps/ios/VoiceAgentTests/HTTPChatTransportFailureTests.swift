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

        transport.discardInterruptedLiveCopy(in: transcript)

        #expect(transport.interruptedTurn == nil)
        #expect(transcript.liveMessages.contains { $0.role == .assistant } == false)
        // The user's turn stays: only the assistant copy was superseded.
        #expect(transcript.liveMessages.map(\.role) == [.user])
    }
}
