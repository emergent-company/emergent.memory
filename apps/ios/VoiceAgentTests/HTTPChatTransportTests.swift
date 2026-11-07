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
struct StubChatStream: ChatStreaming {
    let payloads: [String]
    var error: Error?
    var recorder: StreamCallRecorder?

    func frames(_ turn: ChatTurnRequest) -> AsyncThrowingStream<String, Error> {
        recorder?.calls.append(StreamCall(
            conversationID: turn.conversationID,
            agentDefinitionID: turn.agentDefinitionID,
            message: turn.message,
            surfaceAction: turn.surfaceAction
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
struct StreamCall: Equatable {
    let conversationID: String?
    let agentDefinitionID: String?
    let message: String
    var surfaceAction: ChatSurfaceAction?
}

/// Records what the transport sent into the stream (written from the stub).
final class StreamCallRecorder: @unchecked Sendable {
    nonisolated(unsafe) var calls: [StreamCall] = []
}

/// Records gateway requests the transport makes outside the stream (the
/// best-effort agent-definition read and a decision answer).
private final class RequestRecorder: @unchecked Sendable {
    nonisolated(unsafe) var paths: [String] = []
    nonisolated(unsafe) var methods: [String] = []
    nonisolated(unsafe) var bodies: [Data?] = []
}

/// URLProtocol hands a request body over as a stream, not as `httpBody`; read it
/// so request-shape tests can assert the JSON payload.
private nonisolated func chatRequestBody(_ request: URLRequest) -> Data? {
    if let body = request.httpBody {
        return body
    }
    guard let stream = request.httpBodyStream else { return nil }
    stream.open()
    defer { stream.close() }
    var data = Data()
    let size = 4096
    let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: size)
    defer { buffer.deallocate() }
    while stream.hasBytesAvailable {
        let read = stream.read(buffer, maxLength: size)
        if read <= 0 {
            break
        }
        data.append(buffer, count: read)
    }
    return data
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

    @Test func surfaceActionRidesTheTurnWithoutAUserBubble() async throws {
        let recorder = StreamCallRecorder()
        stubAgentLookup(RequestRecorder())
        defer { HTTPChatStubProtocol.handler = nil }

        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(
            payloads: [ChatStreamFixtures.meta, ChatStreamFixtures.tokenA, ChatStreamFixtures.done],
            recorder: recorder
        )
        let action = ChatSurfaceAction(surfaceID: "s1", componentID: "c1", response: .string("approve"))

        let task = try #require(transport.send(
            "",
            surfaceAction: action,
            conversationID: nil,
            agentName: "alfred",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        // The click is the turn: no empty user bubble, and the action reaches
        // the stream so the server can frame the message it stands for.
        #expect(recorder.calls.map(\.message) == [""])
        #expect(recorder.calls.map(\.surfaceAction) == [action])
        #expect(transcript.liveMessages.map(\.role) == [.assistant])
        #expect(transport.isStreaming == false)
    }

    @Test func emptyTextWithoutASurfaceActionIsDropped() {
        let transport = makeTransport(payloads: [])
        let task = transport.send(
            "   ",
            conversationID: nil,
            agentName: "alfred",
            transcript: ChatSessionTranscriptStore(),
            activity: ChatActivityStore()
        )
        #expect(task == nil)
    }

    @Test func chatRequestCarriesTheSurfaceActionNested() async throws {
        let recorder = RequestRecorder()
        HTTPChatStubProtocol.handler = { request in
            recorder.paths.append(request.url?.path ?? "")
            recorder.methods.append(request.httpMethod ?? "")
            recorder.bodies.append(chatRequestBody(request))
            let response = HTTPURLResponse(
                url: request.url ?? URL(fileURLWithPath: "/"),
                statusCode: 200,
                httpVersion: nil,
                headerFields: ["Content-Type": "text/event-stream"]
            )!
            return (response, Data("data: {\"type\":\"done\"}\n\n".utf8))
        }
        defer { HTTPChatStubProtocol.handler = nil }

        let client = HTTPChatStreamClient(config: MemoryConfig(), session: makeSession())
        let stream = client.frames(ChatTurnRequest(
            conversationID: "conv-1",
            agentDefinitionID: "agent-a",
            message: "",
            surfaceAction: ChatSurfaceAction(surfaceID: "s1", componentID: "c1", response: .string("approve"))
        ))
        for try await _ in stream {}

        let body = try #require(recorder.bodies.first ?? nil)
        let json = try #require(try JSONSerialization.jsonObject(with: body) as? [String: Any])
        #expect(json["message"] as? String == "")
        #expect(json["conversationId"] as? String == "conv-1")
        #expect(json["agentDefinitionId"] as? String == "agent-a")
        let action = try #require(json["surfaceAction"] as? [String: Any])
        #expect(action["surfaceId"] as? String == "s1")
        let payload = try #require(action["action"] as? [String: Any])
        #expect(payload["componentId"] as? String == "c1")
        #expect(payload["response"] as? String == "approve")
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
