import Foundation
@testable import Memory
import Testing

/// A text turn's citations reach its reply, where the Sources footer draws
/// them. The agent name is empty, so the transport makes no agent lookup and
/// the turn needs no network stub.
@MainActor
struct HTTPChatCitationTests {
    private func makeTransport(_ payloads: [String]) -> HTTPChatTransport {
        HTTPChatTransport(
            config: MemoryConfig(),
            stream: StubChatStream(payloads: payloads),
            session: URLSession(configuration: .ephemeral)
        )
    }

    @Test func citationsJoinTheStreamedReply() async throws {
        let transcript = ChatSessionTranscriptStore()
        // Memory sends the citations once, after the answer's tokens.
        let transport = makeTransport([
            ChatStreamFixtures.meta,
            ChatStreamFixtures.tokenA,
            ChatStreamFixtures.citations,
            ChatStreamFixtures.done,
        ])

        let task = try #require(transport.send(
            "who is sam?",
            conversationID: nil,
            agentName: "",
            transcript: transcript,
            activity: ChatActivityStore()
        ))
        await task.value

        let reply = try #require(transcript.liveMessages.last)
        #expect(reply.role == .assistant)
        #expect(reply.text == "Hello")
        #expect(reply.citations.map(\.label) == ["Sam"])
        #expect(reply.citations.first?.objectReference?.ref == "11111111-1111-1111-1111-111111111111")
        // The user's message cites nothing.
        #expect(transcript.liveMessages.first?.citations.isEmpty == true)
    }

    @Test func citationsBeforeAnyTextWaitForTheReply() async throws {
        let transcript = ChatSessionTranscriptStore()
        let transport = makeTransport([
            ChatStreamFixtures.citations,
            ChatStreamFixtures.tokenA,
            ChatStreamFixtures.done,
        ])

        let task = try #require(transport.send(
            "who is sam?",
            conversationID: nil,
            agentName: "",
            transcript: transcript,
            activity: ChatActivityStore()
        ))
        await task.value

        #expect(transcript.liveMessages.last?.citations.map(\.label) == ["Sam"])
    }
}
