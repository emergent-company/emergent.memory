import Foundation
@testable import Memory
import Testing

/// Frames recorded from the gateway's chat stream (`POST /api/chat`), so the
/// parser and mapper are pinned to what the server actually emits rather than
/// to the shapes this client hopes for. Shared with `HTTPChatTransportTests`,
/// which drives the transport over the same bytes.
enum ChatStreamFixtures {
    static let meta = #"{"type":"meta","conversationId":"6f1c2a44-0000-4000-8000-000000000001","citations":[]}"#
    static let metaWithoutConversation = #"{"type":"meta"}"#
    static let tokenA = #"{"type":"token","token":"Hello"}"#
    static let tokenB = #"{"type":"token","token":" there"}"#
    static let html = #"{"type":"html","html":"<p>Hello there</p>"}"#
    static let thinking = #"{"type":"thinking","id":"t1","role":"reasoning","text":"Checking the graph.","done":false}"#
    static let thinkingDone = #"{"type":"thinking","id":"t1","role":"reasoning","text":"","done":true}"#
    static let subagent = #"{"type":"subagent","id":"s1","parentRunId":"p1","runId":"r1","agentName":"Researcher","task":"scan","status":"running"}"#
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

struct SSEFrameParserTests {
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

struct ChatStreamFrameDecoderTests {
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
        #expect(decode(ChatStreamFixtures.thinking) == .activity(.thinking(id: "t1", delta: "Checking the graph.")))
        // A segment-closing frame carries no new text.
        #expect(decode(ChatStreamFixtures.thinkingDone) == nil)
    }

    @Test func subagentFrameBecomesActivity() {
        #expect(decode(ChatStreamFixtures.subagent) == .activity(.subagent(ChatSubagentEvent(
            id: "s1",
            parentRunId: "p1",
            runId: "r1",
            agentName: "Researcher",
            task: "scan",
            status: "running",
            error: nil
        ))))
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
            .activity(.thinking(id: "t1", delta: "replayed")),
            .done,
        ]))
    }
}
