import Foundation
@testable import Memory
import Testing

/// Frames recorded from the gateway's chat stream (`POST /api/chat`), so the
/// parser and mapper are pinned to what the server actually emits rather than
/// to the shapes this client hopes for.
enum ChatStreamFixtures {
    static let meta = #"{"type":"meta","conversationId":"6f1c2a44-0000-4000-8000-000000000001","citations":[]}"#
    static let metaWithoutConversation = #"{"type":"meta"}"#
    static let tokenA = #"{"type":"token","token":"Hello"}"#
    static let tokenB = #"{"type":"token","token":" there"}"#
    static let html = #"{"type":"html","html":"<p>Hello there</p>"}"#
    static let thinking = #"{"type":"thinking","id":"t1","role":"reasoning","text":"Checking the graph.","done":false}"#
    static let thinkingDone = #"{"type":"thinking","id":"t1","role":"reasoning","text":"","done":true}"#
    static let toolStarted = #"{"type":"mcp_tool","tool":"search","status":"started","id":"call-1"}"#
    static let toolCompleted = #"{"type":"mcp_tool","tool":"search","status":"completed","id":"call-1","result":{"count":2}}"#
    static let approval = #"{"type":"approval","tool":"delete_document","input":{"id":"doc-1"},"questionId":"q-approve"}"#
    static let question = #"{"type":"question","questionId":"q-ask","question":"Which project?","interactionType":"text","options":[],"placeholder":"Type a name","maxLength":60}"#
    static let questionButtons = #"{"type":"question","questionId":"q-pick","question":"Pick one","interactionType":"buttons","options":[{"label":"A","value":"a"},{"label":"B","value":"b","description":"second"}]}"#
    static let surface = #"{"type":"ui","surfaceId":"s1","messages":[{"createSurface":{"surfaceId":"s1","catalogId":"catalog"}}]}"#
    static let citations = #"{"type":"citations","citations":[{"title":"Doc"}]}"#
    static let failure = #"{"type":"error","error":"The agent run was interrupted."}"#
    static let done = #"{"type":"done"}"#
    static let unknown = #"{"type":"future_frame","whatever":1}"#
    static let liveReplay = #"{"type":"live_replay","frames":[{"type":"thinking","id":"t1","role":"reasoning","text":"replayed","done":false},{"type":"done"}]}"#
}

@Suite struct SSEFrameParserTests {
    @Test func dispatchesOnlyOnBlankLine() {
        var parser = SSEFrameParser()
        #expect(parser.consume(#"data: {"type":"token","token":"hi"}"#) == nil)
        #expect(parser.consume("") == #"{"type":"token","token":"hi"}"#)
        #expect(parser.consume("") == nil)
    }

    @Test func ignoresCommentsAndOtherFields() {
        var parser = SSEFrameParser()
        #expect(parser.consume(": ping") == nil)
        #expect(parser.consume("event: message") == nil)
        #expect(parser.consume(#"data: {"type":"done"}"#) == nil)
        #expect(parser.consume("") == #"{"type":"done"}"#)
    }

    @Test func acceptsDataWithoutSpace() {
        var parser = SSEFrameParser()
        _ = parser.consume("data:{}")
        #expect(parser.consume("") == "{}")
    }

    @Test func joinsMultiLineData() {
        var parser = SSEFrameParser()
        _ = parser.consume("data: line-one")
        _ = parser.consume("data: line-two")
        #expect(parser.consume("") == "line-one\nline-two")
    }

    @Test func finishFlushesTrailingPayload() {
        var parser = SSEFrameParser()
        _ = parser.consume(#"data: {"type":"done"}"#)
        #expect(parser.finish() == #"{"type":"done"}"#)
        #expect(parser.finish() == nil)
    }
}

@Suite struct ChatStreamFrameDecoderTests {
    private func decode(_ payload: String) -> ChatStreamEvent? {
        ChatStreamFrameDecoder.decode(payload, fallbackToolID: { "fallback" })
    }

    @Test func metaCarriesTheConversation() {
        #expect(decode(ChatStreamFixtures.meta) == .meta(
            conversationID: "6f1c2a44-0000-4000-8000-000000000001",
            runID: nil
        ))
        #expect(decode(ChatStreamFixtures.metaWithoutConversation) == .meta(conversationID: nil, runID: nil))
    }

    @Test func tokenIsRawText() {
        #expect(decode(ChatStreamFixtures.tokenA) == .text("Hello"))
        #expect(decode(ChatStreamFixtures.tokenB) == .text(" there"))
    }

    @Test func htmlSnapshotIsDropped() {
        #expect(decode(ChatStreamFixtures.html) == nil)
    }

    @Test func thinkingDeltasBecomeActivity() {
        #expect(decode(ChatStreamFixtures.thinking) == .activity(.thinking(delta: "Checking the graph.")))
        // A segment-closing frame carries no new text.
        #expect(decode(ChatStreamFixtures.thinkingDone) == nil)
    }

    @Test func toolFramesCorrelateOnTheServerId() {
        #expect(decode(ChatStreamFixtures.toolStarted) == .activity(.toolCall(ChatToolCallEvent(
            id: "call-1",
            tool: "search",
            arguments: ""
        ))))
        guard case let .activity(.toolResult(result))? = decode(ChatStreamFixtures.toolCompleted) else {
            Issue.record("expected a tool result")
            return
        }
        #expect(result.id == "call-1")
        #expect(result.tool == "search")
        #expect(result.isError == false)
        #expect(result.result.contains("count"))
    }

    @Test func toolFrameWithoutIdGetsTheFallback() {
        guard case let .activity(.toolCall(call))? = decode(#"{"type":"mcp_tool","tool":"search","status":"started"}"#) else {
            Issue.record("expected a tool call")
            return
        }
        #expect(call.id == "fallback")
    }

    @Test func approvalCarriesItsQuestionIdAndArguments() {
        guard case let .activity(.approval(approval))? = decode(ChatStreamFixtures.approval) else {
            Issue.record("expected an approval")
            return
        }
        #expect(approval.questionId == "q-approve")
        #expect(approval.tool == "delete_document")
        #expect(approval.arguments.contains("doc-1"))
    }

    @Test func questionMapsTheHttpInteractionType() {
        guard case let .activity(.question(question))? = decode(ChatStreamFixtures.question) else {
            Issue.record("expected a question")
            return
        }
        #expect(question.questionId == "q-ask")
        #expect(question.interactionType == .freeText)
        #expect(question.placeholder == "Type a name")
        #expect(question.maxLength == 60)
    }

    @Test func questionButtonsKeepTheirOptions() {
        guard case let .activity(.question(question))? = decode(ChatStreamFixtures.questionButtons) else {
            Issue.record("expected a question")
            return
        }
        #expect(question.interactionType == .buttons)
        #expect(question.options.map(\.value) == ["a", "b"])
        #expect(question.options.last?.description == "second")
    }

    @Test func surfaceDecodesIntoTheA2UIModel() {
        guard case let .activity(.ui(surface))? = decode(ChatStreamFixtures.surface) else {
            Issue.record("expected a surface")
            return
        }
        #expect(surface.surfaceId == "s1")
        #expect(surface.messages.first?.createSurface?.surfaceId == "s1")
    }

    @Test func citationsAndFailureAndDone() {
        #expect(decode(ChatStreamFixtures.citations) == .citations(count: 1))
        #expect(decode(ChatStreamFixtures.failure) == .failure("The agent run was interrupted."))
        #expect(decode(ChatStreamFixtures.done) == .done)
    }

    @Test func unknownFramesAreIgnored() {
        #expect(decode(ChatStreamFixtures.unknown) == nil)
        #expect(decode("not json") == nil)
    }

    @Test func liveReplayFlattensItsFrames() {
        #expect(decode(ChatStreamFixtures.liveReplay) == .replay([
            .activity(.thinking(delta: "replayed")),
            .done,
        ]))
    }
}

// MARK: - Transport behaviour

/// A `ChatStreaming` stub that yields a fixed frame list.
private struct StubChatStream: ChatStreaming {
    let payloads: [String]
    var error: Error?

    func frames(
        conversationID _: String?,
        agentDefinitionID _: String?,
        message _: String
    ) -> AsyncThrowingStream<String, Error> {
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

/// Serialized because the URLProtocol stub's handler is shared.
@Suite(.serialized) @MainActor struct HTTPChatTransportTests {
    private func makeSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [GatewayHTTPStubProtocol.self]
        return URLSession(configuration: configuration)
    }

    private func makeTransport(
        payloads: [String],
        error: Error? = nil
    ) -> HTTPChatTransport {
        HTTPChatTransport(
            config: MemoryConfig(),
            stream: StubChatStream(payloads: payloads, error: error),
            session: makeSession()
        )
    }

    @Test func textTurnRendersWithoutAnyRoom() async throws {
        GatewayHTTPStubProtocol.handler = { request in
            // The only request a text turn may make is the agent-definition read
            // (best-effort); it must never be the token mint.
            #expect(request.url?.path != "/api/token")
            return (
                HTTPURLResponse(url: request.url!, statusCode: 404, httpVersion: nil, headerFields: nil)!,
                Data()
            )
        }
        defer { GatewayHTTPStubProtocol.handler = nil }

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

        // The transcript holds the user's message and the streamed reply.
        #expect(transcript.liveMessages.map(\.role) == [.user, .assistant])
        #expect(transcript.liveMessages.last?.text == "Hello there")

        // The learned conversation is remembered.
        #expect(transport.conversationID == "6f1c2a44-0000-4000-8000-000000000001")
        #expect(transport.isStreaming == false)

        // Activity: one turn anchored to the user message, with thinking and a
        // settled tool call.
        let turn = try #require(activity.liveTurn)
        #expect(turn.anchorMessageID == transcript.liveMessages.first?.id)
        #expect(turn.thinkingText == "Checking the graph.")
        #expect(turn.tools.count == 1)
        #expect(turn.tools.first?.isRunning == false)
        #expect(turn.didReplyStart)
    }

    @Test func learnedConversationIsReportedOnce() async throws {
        let transcript = ChatSessionTranscriptStore()
        let activity = ChatActivityStore()
        let transport = makeTransport(payloads: [ChatStreamFixtures.meta, ChatStreamFixtures.tokenA, ChatStreamFixtures.done])
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

        #expect(learned == ["6f1c2a44-0000-4000-8000-000000000001"])
    }

    @Test func decisionSurfaceAnswersOverTheQuestionRoute() async throws {
        let transport = makeTransport(payloads: [])
        let captured = RequestCapture()
        GatewayHTTPStubProtocol.handler = { request in
            captured.record(request)
            return (
                HTTPURLResponse(url: request.url!, statusCode: 202, httpVersion: nil, headerFields: nil)!,
                Data(#"{"questionId":"q-ask","status":"ok"}"#.utf8)
            )
        }
        defer { GatewayHTTPStubProtocol.handler = nil }

        try await transport.respond(questionID: "q-ask", response: "graph", message: "")

        let request = try #require(captured.requests.first)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path == "/api/chat/questions/q-ask/respond")
    }

    @Test func stopReadsTheRunIdFromTheTimelineAndCancelsIt() async throws {
        let transport = makeTransport(payloads: [])
        let activity = ChatActivityStore()
        let captured = RequestCapture()
        GatewayHTTPStubProtocol.handler = { request in
            captured.record(request)
            if request.url?.path == "/api/conversations/c1/history" {
                let body = #"""
                {"items":[{"kind":"run_start","run_id":"run-early"},{"kind":"run_end","run_id":"run-latest","run_status":"running"}]}
                """#
                return (
                    HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!,
                    Data(body.utf8)
                )
            }
            return (
                HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!,
                Data(#"{"ok":true}"#.utf8)
            )
        }
        defer { GatewayHTTPStubProtocol.handler = nil }

        // Seed the conversation the turn belongs to.
        let transcript = ChatSessionTranscriptStore()
        let task = try #require(transport.send(
            "hello",
            conversationID: "c1",
            agentName: "",
            transcript: transcript,
            activity: activity
        ))
        await task.value

        await transport.stop(activity: activity)

        let cancel = try #require(captured.requests.last)
        #expect(cancel.httpMethod == "POST")
        #expect(cancel.url?.path == "/api/chat/runs/run-latest/cancel")
        #expect(activity.isGeneratingReply == false)
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
}

/// Captures requests made through the URLProtocol stub (the handler runs off
/// the main actor).
private nonisolated final class RequestCapture: @unchecked Sendable {
    private let lock = NSLock()
    private var storage: [URLRequest] = []

    var requests: [URLRequest] {
        lock.lock()
        defer { lock.unlock() }
        return storage
    }

    func record(_ request: URLRequest) {
        lock.lock()
        defer { lock.unlock() }
        storage.append(request)
    }
}
