import Foundation
@testable import Memory
import Testing

// MARK: - Fixtures

//
// Frames shaped exactly as the gateway's `/api/chat` stream emits them: the
// `mcp_tool` frame enriched with `toolCategory`, `toolSubject` and `toolView`
// by the gateway's `toolresult` package. These are the same values the web
// console's chip and row read, so decoding them is what makes one call read the
// same on both surfaces.

private enum ToolFrameFixtures {
    static let category = #"{"family":"search","verb":"Searched memory","runningVerb":"Searching memory","icon":"lucide--search"}"#

    static let started = #"{"type":"mcp_tool","tool":"search_entities","status":"started","id":"call-1","result":{"query":"d1"},"toolCategory":\#(category),"toolSubject":"d1"}"#

    static let completedWithRecords = #"""
    {"type":"mcp_tool","tool":"search_entities","status":"completed","id":"call-1",
     "result":{"results":[]},
     "toolCategory":\#(category),
     "toolView":{"kind":"records","items":[{"title":"Retrieval guide","detail":"document"}],"moreRecords":0,"outcome":"1 record","raw":"{\"results\":[]}"}}
    """#

    /// The server says the list is empty over a payload that would parse to
    /// records locally. The server's answer is the one both surfaces draw.
    static let completedEmpty = #"""
    {"type":"mcp_tool","tool":"search_entities","status":"completed","id":"call-1",
     "result":{"results":[{"id":"d1","title":"Locally visible"}]},
     "toolCategory":{"family":"search","verb":"Searched memory","runningVerb":"Searching memory","icon":"lucide--search"},
     "toolView":{"kind":"empty","text":"No results","outcome":"No results","raw":"{\"results\":[]}"}}
    """#

    /// A gateway that predates the enrichment: no category, subject or view.
    static let legacyCompleted = #"{"type":"mcp_tool","tool":"web-fetch","status":"completed","id":"call-1","result":{"results":[{"title":"Swift concurrency","url":"https://example.com"}]}}"#
}

// MARK: - The gateway's word wins

/// The two surfaces render one presentation. Where the gateway says what a call
/// is, what it was asked to do, and what came back, the app draws that — it does
/// not classify or parse the call a second time and hope the two agree.
struct ToolServerPresentationTests {
    @MainActor
    @Test func startedFrameCarriesTheCategoryAndSubjectIntoTheRow() throws {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        try store.apply(.toolCall(startedEvent()))

        let tool = try #require(store.liveTurn?.tools.first)
        #expect(tool.category == .search)
        // The row calls itself the way the web chip does: server verb, then
        // server subject in one line.
        #expect(tool.subject == "d1")
    }

    /// A name the app's table does not know is still classified: the gateway
    /// knows, and the app takes its answer.
    @MainActor
    @Test func serverFamilyOutranksTheLocalNameTable() throws {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.toolCall(ChatToolCallEvent(
            id: "call-9",
            tool: "wire_name_this_build_has_never_seen",
            arguments: "{}",
            category: ChatToolCategory(
                family: "documents",
                verb: "Worked with documents",
                runningVerb: "Working with documents",
                icon: "lucide--file-text"
            ),
            subject: "retrieval.md"
        )))

        let tool = try #require(store.liveTurn?.tools.first)
        #expect(tool.category == .documents)
        #expect(tool.subject == "retrieval.md")
    }

    @MainActor
    @Test func settledFrameDrawsTheServersReadableView() throws {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        try store.apply(.toolCall(startedEvent()))
        try store.apply(.toolResult(completedEvent(ToolFrameFixtures.completedWithRecords)))

        let tool = try #require(store.liveTurn?.tools.first)
        let presentation = try #require(tool.presentation)
        #expect(presentation.body == .items(
            [ToolResultPresentation.Item(id: 0, title: "Retrieval guide", detail: "document", url: nil)],
            more: 0
        ))
        #expect(tool.isRunning == false)
        // The subject the running frame carried survives the settle, exactly as
        // it does on the web row, which keeps its label across the two frames.
        #expect(tool.subject == "d1")
        #expect(tool.category == .search)
    }

    /// The server's presentation outranks a local parse of the same payload, so
    /// a result the two would read differently cannot read two ways.
    @MainActor
    @Test func serverViewOutranksTheLocalParse() throws {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        try store.apply(.toolCall(startedEvent()))
        try store.apply(.toolResult(completedEvent(ToolFrameFixtures.completedEmpty)))

        let tool = try #require(store.liveTurn?.tools.first)
        #expect(tool.presentation?.body == .empty)
        // The local parse would have found the record in the raw payload.
        #expect(ToolResultPresentation.parse(tool.result)?.body != .empty)
    }

    /// A gateway without the enrichment still works: the app classifies the name,
    /// reads the arguments and parses the result on its own, as it always did.
    @MainActor
    @Test func legacyFrameFallsBackToLocalClassification() throws {
        let frame = try #require(
            ChatStreamFrameDecoder.decode(ToolFrameFixtures.legacyCompleted, fallbackToolID: { "fallback" })
        )
        guard case let .activity(.toolResult(result)) = frame else {
            Issue.record("expected a tool result")
            return
        }
        #expect(result.category == nil)
        #expect(result.view == nil)

        let tool = ToolCallInfo(
            id: result.id,
            name: result.tool,
            arguments: #"{"query":"swift concurrency"}"#,
            result: result.result,
            isError: result.isError,
            isRunning: false
        )
        #expect(tool.category == .web)
        #expect(tool.subject == "swift concurrency")
        #expect(tool.presentation?.body != nil)
    }

    /// The expanded step's meta line is the web console's: duration, response
    /// size and the call id, in that order, each part only when it exists.
    @Test func metaLineReportsDurationSizeAndIdentity() {
        let tool = ToolCallInfo(
            id: "call-1",
            name: "search_entities",
            arguments: "{}",
            result: "ok",
            isError: false,
            isRunning: false,
            duration: 1.2,
            responseSize: 2
        )
        #expect(tool.metaDurationText == "1.2s")
        #expect(tool.responseSizeText == "2 B")
        #expect(tool.idText == "#call-1")

        let running = ToolCallInfo(
            id: "",
            name: "search_entities",
            arguments: "{}",
            result: nil,
            isError: false,
            isRunning: true
        )
        #expect(running.metaDurationText == nil)
        #expect(running.responseSizeText == nil)
        #expect(running.idText == nil)
    }

    /// A failed frame's fallback text (the error, or the status word) is not a
    /// response: the meta line must not report its size as one.
    @Test func failureAndStatusTextAreNotMeasuredAsAResponse() throws {
        let failed = try #require(
            ChatStreamFrameDecoder.decode(
                #"{"type":"mcp_tool","tool":"search_entities","status":"error","id":"call-1","error":"boom"}"#,
                fallbackToolID: { "fallback" }
            )
        )
        guard case let .activity(.toolResult(failedResult)) = failed else {
            Issue.record("expected a failed tool result")
            return
        }
        #expect(failedResult.result == "boom")
        #expect(failedResult.outputSize == nil)

        let noResult = try #require(
            ChatStreamFrameDecoder.decode(
                #"{"type":"mcp_tool","tool":"search_entities","status":"completed","id":"call-1"}"#,
                fallbackToolID: { "fallback" }
            )
        )
        guard case let .activity(.toolResult(noResultEvent)) = noResult else {
            Issue.record("expected a settled tool result")
            return
        }
        #expect(noResultEvent.result == "completed")
        #expect(noResultEvent.outputSize == nil)
    }

    @Test func wordsResponseSizesLikeTheWebConsole() {
        #expect(ToolResultSize.text(bytes: 0) == "0 B")
        #expect(ToolResultSize.text(bytes: 512) == "512 B")
        #expect(ToolResultSize.text(bytes: 1024) == "1 KB")
        #expect(ToolResultSize.text(bytes: 1200) == "1.2 KB")
        #expect(ToolResultSize.text(bytes: 1_048_576) == "1 MB")
        #expect(ToolResultSize.text(bytes: 3_600_000) == "3.4 MB")
        #expect(ToolResultSize.text(bytes: nil) == nil)
    }

    // MARK: - Helpers

    private func startedEvent() throws -> ChatToolCallEvent {
        let frame = try #require(
            ChatStreamFrameDecoder.decode(ToolFrameFixtures.started, fallbackToolID: { "fallback" })
        )
        guard case let .activity(.toolCall(call)) = frame else {
            throw TestFailure.wrongFrame
        }
        return call
    }

    private func completedEvent(_ payload: String) throws -> ChatToolResultEvent {
        let frame = try #require(
            ChatStreamFrameDecoder.decode(payload, fallbackToolID: { "fallback" })
        )
        guard case let .activity(.toolResult(result)) = frame else {
            throw TestFailure.wrongFrame
        }
        return result
    }

    private enum TestFailure: Error {
        case wrongFrame
    }
}
