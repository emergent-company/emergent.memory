import Foundation
@testable import Memory
import Testing

/// A `URLProtocol` stub for this suite only. `GatewayHTTPTests` and
/// `SessionLogClientTests` own their own: `@Suite(.serialized)` serializes a
/// suite, not the whole run, so a shared stub handler would be clobbered by a
/// suite running in parallel.
final nonisolated class HTTPChatStubProtocol: URLProtocol {
    nonisolated(unsafe) static var handler: (@Sendable (URLRequest) throws -> (URLResponse, Data))?

    override static func canInit(with _: URLRequest) -> Bool {
        true
    }

    override static func canonicalRequest(for request: URLRequest) -> URLRequest {
        request
    }

    override func startLoading() {
        guard let handler = Self.handler else {
            client?.urlProtocol(self, didFailWithError: URLError(.unsupportedURL))
            return
        }
        do {
            let (response, data) = try handler(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}
}

/// A `ChatStreaming` stub: yields a fixed frame list and records what the
/// transport asked for, so continuation and the request shape are assertable
/// without a network.
private struct StubChatStream: ChatStreaming {
    let payloads: [String]
    var error: Error?
    var recorder: StreamCallRecorder?

    func frames(
        conversationID: String?,
        agentDefinitionID: String?,
        message: String
    ) -> AsyncThrowingStream<String, Error> {
        recorder?.calls.append(StreamCall(
            conversationID: conversationID,
            agentDefinitionID: agentDefinitionID,
            message: message
        ))
        let payloads = payloads
        let error = error
        return AsyncThrowingStream { continuation in
            for payload in payloads {
                continuation.yield(payload)
            }
            continuation.finish(throwing: error)
        }
    }
}

/// One `send` as the transport handed it to the stream.
private struct StreamCall: Equatable {
    let conversationID: String?
    let agentDefinitionID: String?
    let message: String
}

/// Records what the transport sent into the stream (written from the stub).
private final class StreamCallRecorder: @unchecked Sendable {
    nonisolated(unsafe) var calls: [StreamCall] = []
}

/// Records gateway requests the transport makes outside the stream (the
/// best-effort agent-definition read and a decision answer).
private final class RequestRecorder: @unchecked Sendable {
    nonisolated(unsafe) var paths: [String] = []
    nonisolated(unsafe) var methods: [String] = []
}

/// Serialized because the stub's static handler and recorders are shared.
@Suite(.serialized) @MainActor struct HTTPChatTransportTests {
    private let learnedConversationID = "6f1c2a44-0000-4000-8000-000000000001"

    private func makeSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [HTTPChatStubProtocol.self]
        return URLSession(configuration: configuration)
    }

    private func makeTransport(
        payloads: [String],
        error: Error? = nil,
        recorder: StreamCallRecorder? = nil
    ) -> HTTPChatTransport {
        HTTPChatTransport(
            config: MemoryConfig(),
            stream: StubChatStream(payloads: payloads, error: error, recorder: recorder),
            session: makeSession()
        )
    }

    /// Answers the best-effort agent lookup with a miss, and records the paths
    /// the transport asked for.
    private func stubAgentLookup(_ recorder: RequestRecorder) {
        HTTPChatStubProtocol.handler = { request in
            recorder.paths.append(request.url?.path ?? "")
            recorder.methods.append(request.httpMethod ?? "")
            let response = HTTPURLResponse(
                url: request.url ?? URL(fileURLWithPath: "/"),
                statusCode: 404,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data())
        }
    }

    @Test func textTurnRendersWithoutAnyRoom() async throws {
        let recorder = RequestRecorder()
        stubAgentLookup(recorder)
        defer { HTTPChatStubProtocol.handler = nil }

        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(payloads: [
            ChatStreamFixtures.meta,
            ChatStreamFixtures.thinking,
            ChatStreamFixtures.toolStarted,
            ChatStreamFixtures.tokenA,
            ChatStreamFixtures.tokenB,
            ChatStreamFixtures.toolCompleted,
            ChatStreamFixtures.done,
        ])

        let task = try #require(transport.send(
            "hello",
            conversationID: nil,
            agentName: "alfred",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        // A text turn must never mint a room token.
        #expect(!recorder.paths.contains("/api/token"))

        // The transcript holds the user's message and the streamed reply.
        #expect(transcript.liveMessages.map(\.role) == [.user, .assistant])
        #expect(transcript.liveMessages.last?.text == "Hello there")

        // The learned conversation is remembered.
        #expect(transport.conversationID == learnedConversationID)
        #expect(transport.isStreaming == false)

        // Activity: one turn anchored to the user message, with thinking and a
        // settled tool call.
        let turn = try #require(activity.liveTurn)
        #expect(turn.anchorMessageID == transcript.liveMessages.first?.id)
        #expect(turn.thinkingBlocks.map(\.text) == ["Checking the graph."])
        #expect(turn.tools.count == 1)
        #expect(turn.tools.first?.isRunning == false)
        #expect(turn.didReplyStart)
    }

    @Test func secondTurnContinuesTheLearnedConversation() async throws {
        let recorder = StreamCallRecorder()
        stubAgentLookup(RequestRecorder())
        defer { HTTPChatStubProtocol.handler = nil }

        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(
            payloads: [ChatStreamFixtures.meta, ChatStreamFixtures.tokenA, ChatStreamFixtures.done],
            recorder: recorder
        )

        for message in ["first", "second"] {
            let task = try #require(transport.send(
                message,
                conversationID: transport.conversationID,
                agentName: "alfred",
                transcript: transcript,
                activity: activity
            ))
            await task.value
        }

        // A new chat's id comes from `meta`; the next turn must continue it
        // rather than let the server create a second conversation.
        #expect(recorder.calls.map(\.conversationID) == [nil, learnedConversationID])
        #expect(recorder.calls.map(\.message) == ["first", "second"])
    }

    @Test func learnedConversationIsReportedOnce() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(payloads: [
            ChatStreamFixtures.meta,
            ChatStreamFixtures.tokenA,
            ChatStreamFixtures.done,
        ])
        stubAgentLookup(RequestRecorder())
        defer { HTTPChatStubProtocol.handler = nil }
        var learned: [String] = []
        transport.onConversationLearned = { learned.append($0) }

        let task = try #require(transport.send(
            "hello",
            conversationID: nil,
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        #expect(learned == [learnedConversationID])
    }

    @Test func decisionSurfaceAnswersOverTheQuestionRoute() async throws {
        let transport = makeTransport(payloads: [])
        let recorder = RequestRecorder()
        HTTPChatStubProtocol.handler = { request in
            recorder.paths.append(request.url?.path ?? "")
            recorder.methods.append(request.httpMethod ?? "")
            let response = HTTPURLResponse(
                url: request.url ?? URL(fileURLWithPath: "/"),
                statusCode: 202,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data(#"{"questionId":"q-ask","status":"ok"}"#.utf8))
        }
        defer { HTTPChatStubProtocol.handler = nil }

        try await transport.respond(questionID: "q-ask", response: "graph", message: "")

        #expect(recorder.methods == ["POST"])
        #expect(recorder.paths == ["/api/chat/questions/q-ask/respond"])
    }

    @Test func turnFailureIsReportedAsATurnError() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(payloads: [ChatStreamFixtures.failure])
        stubAgentLookup(RequestRecorder())
        defer { HTTPChatStubProtocol.handler = nil }

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
        stubAgentLookup(RequestRecorder())
        defer { HTTPChatStubProtocol.handler = nil }

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

    /// A stream that dies mid-turn leaves a run still going server-side, so the
    /// view has to reload what Memory recorded — signalled by the counter.
    @Test func aStreamThatDiesMidTurnIsSignalledForReloading() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(
            payloads: [ChatStreamFixtures.meta, ChatStreamFixtures.tokenA],
            error: URLError(.networkConnectionLost)
        )
        stubAgentLookup(RequestRecorder())
        defer { HTTPChatStubProtocol.handler = nil }

        let task = try #require(transport.send(
            "hello",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        #expect(transport.droppedStreamCount == 1)
        #expect(transport.errorMessage != nil)
        #expect(transport.isStreaming == false)
        // What did arrive is kept: the user's turn and the partial reply.
        #expect(transcript.liveMessages.map(\.role) == [.user, .assistant])
    }

    /// A turn the gateway refused never ran, so there is nothing to reload and
    /// the counter must stay put — otherwise every 401 would re-fetch history.
    @Test func aRefusedTurnIsNotSignalledAsADrop() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(
            payloads: [],
            error: GatewayError.httpStatus(401, "Chat returned HTTP 401: session_required")
        )
        stubAgentLookup(RequestRecorder())
        defer { HTTPChatStubProtocol.handler = nil }

        let task = try #require(transport.send(
            "hello",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        #expect(transport.droppedStreamCount == 0)
        #expect(transport.errorMessage != nil)
    }

    @Test func resetDropsTheTurnAndTheConversation() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(payloads: [
            ChatStreamFixtures.meta,
            ChatStreamFixtures.tokenA,
            ChatStreamFixtures.done,
        ])
        stubAgentLookup(RequestRecorder())
        defer { HTTPChatStubProtocol.handler = nil }

        let task = try #require(transport.send(
            "hello",
            conversationID: nil,
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value
        guard transport.conversationID != nil else {
            Issue.record("expected a learned conversation")
            return
        }

        transport.reset()

        #expect(transport.conversationID == nil)
        #expect(transport.isStreaming == false)
    }
}
