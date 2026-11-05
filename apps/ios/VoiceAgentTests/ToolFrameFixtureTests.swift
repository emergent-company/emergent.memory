import Foundation
@testable import Memory
import Testing

// MARK: - Shared contract fixture

struct ToolFrameFixtureTests {
    /// The checked-in frames the Go gateway test pins too
    /// (`apps/web-ui/gateway/tool_frame_fixture_test.go`): the gateway must emit
    /// them, and this client must draw them, or one of the two tests fails.
    @MainActor
    @Test func theSharedStartedFrameCarriesTheServersAnswer() throws {
        let frames = try sharedFrames()
        #expect(frames.count == 2)
        let call = try startedCall(frames[0])

        // Assert the decoded wire values, not only the row derived from them: a
        // dropped decode fails here rather than silently matching the app's own
        // fallback.
        #expect(call.category == expectedCategory)
        #expect(call.subject == "Retrieval guide")
        // The app's own reading of the same arguments says something else, so
        // this really is the server's answer.
        #expect(ToolActivitySubject.line(from: call.arguments) == "https://example.com/guide")

        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.toolCall(call))
        let tool = try #require(store.liveTurn?.tools.last)
        #expect(tool.category == .search)
        #expect(tool.verbText == "Searching memory")
        #expect(tool.subject == "Retrieval guide")
    }

    @MainActor
    @Test func theSharedSettledFrameDrawsTheServersView() throws {
        let frames = try sharedFrames()
        #expect(frames.count == 2)
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        let started = try startedCall(frames[0])
        store.apply(.toolCall(started))

        let result = try settledResult(frames[1])
        #expect(result.category == expectedCategory)
        #expect(result.view?.kind == "records")
        #expect(result.view?.items?.count == 1)
        #expect(result.view?.items?.first?.title == "Retrieval guide")
        #expect(result.view?.items?.first?.detail == nil)
        #expect(result.view?.items?.first?.link == "https://example.com/guide")
        #expect(result.view?.moreRecords == nil)
        #expect(result.view?.outcome == "1 record")

        store.apply(.toolResult(result))
        let tool = try #require(store.liveTurn?.tools.last)
        #expect(tool.doneVerbText == "Searched memory")
        // The running frame's subject survives the settle.
        #expect(tool.subject == "Retrieval guide")
        #expect(tool.isError == false)
        #expect(tool.presentation?.body == .items(
            [ToolResultPresentation.Item(
                id: 0,
                title: "Retrieval guide",
                detail: nil,
                url: "https://example.com/guide"
            )],
            more: 0
        ))
        // The raw payload the server bounded is the one the disclosure reveals,
        // not a local re-print of it.
        #expect(tool.presentation?.raw == frames[1].expectedRaw)
    }

    private var expectedCategory: ChatToolCategory {
        ChatToolCategory(
            family: "search",
            verb: "Searched memory",
            runningVerb: "Searching memory",
            icon: "lucide--search"
        )
    }

    private func startedCall(_ frame: SharedFrame) throws -> ChatToolCallEvent {
        let event = try #require(
            ChatStreamFrameDecoder.decode(frame.received, fallbackToolID: { "fallback" })
        )
        guard case let .activity(.toolCall(call)) = event else {
            throw FixtureFailure.wrongFrame
        }
        return call
    }

    private func settledResult(_ frame: SharedFrame) throws -> ChatToolResultEvent {
        let event = try #require(
            ChatStreamFrameDecoder.decode(frame.received, fallbackToolID: { "fallback" })
        )
        guard case let .activity(.toolResult(result)) = event else {
            throw FixtureFailure.wrongFrame
        }
        return result
    }

    private enum FixtureFailure: Error {
        case wrongFrame
    }

    /// One fixture entry as the client receives it: the worker frame with the
    /// gateway's enrichment applied in place, exactly as `emitToolResultHTML`
    /// does (the Go test proves the enrichment values).
    private struct SharedFrame {
        let received: String
        let expectedRaw: String?
    }

    private func sharedFrames() throws -> [SharedFrame] {
        let url = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .appendingPathComponent("Fixtures/tool-call-frames.json")
        let object = try JSONSerialization.jsonObject(with: Data(contentsOf: url))
        let root = try #require(object as? [String: Any])
        let frames = try #require(root["frames"] as? [[String: Any]])
        return try frames.map { frame in
            var received = try #require(frame["upstream"] as? [String: Any])
            let expected = try #require(frame["expected"] as? [String: Any])
            for (key, value) in expected {
                received[key] = value
            }
            let data = try JSONSerialization.data(withJSONObject: received)
            let receivedJSON = try #require(String(data: data, encoding: .utf8))
            let view = expected["toolView"] as? [String: Any]
            return SharedFrame(
                received: receivedJSON,
                expectedRaw: view?["raw"] as? String
            )
        }
    }

    /// A category or view that loses a nested field degrades that piece; it must
    /// not drop the whole tool frame on the floor.
    @Test func partialEnrichmentStillDecodesTheCall() throws {
        let payload = #"""
        {"type":"mcp_tool","tool":"search_entities","status":"completed","id":"call-1",
         "result":{"name":"Retrieval guide"},
         "toolCategory":{"family":"search"},
         "toolView":{"kind":"records","items":[{"link":"https://example.com/guide"}],"raw":"{}"}}
        """#
        let event = try #require(
            ChatStreamFrameDecoder.decode(payload, fallbackToolID: { "fallback" })
        )
        guard case let .activity(.toolResult(result)) = event else {
            Issue.record("expected a tool result")
            return
        }
        #expect(result.category?.family == "search")
        #expect(result.category?.verb == "")
        #expect(result.view?.kind == "records")
        #expect(result.view?.items?.first?.title == "")

        let tool = ToolCallInfo(
            id: result.id,
            name: result.tool,
            arguments: "{}",
            result: result.result,
            isError: result.isError,
            isRunning: false,
            category: result.category
        )
        // The blank server verb falls back to the catalogue rather than
        // drawing an empty row.
        #expect(tool.doneVerbText == "Searched memory")
    }
}
