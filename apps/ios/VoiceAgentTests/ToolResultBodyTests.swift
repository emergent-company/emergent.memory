import Foundation
@testable import Memory
import SwiftUI
import Testing
import UIKit

// MARK: - Model

struct ToolCallPresentationTests {
    @Test func aSettledCallCarriesItsReadableResult() throws {
        let tool = ToolCallInfo(
            id: "t1",
            name: "search-hybrid",
            arguments: "{}",
            result: Payload.search,
            isError: false
        )
        let body = try #require(tool.presentation?.body)
        guard case let .items(items, _) = body else {
            Issue.record("expected items, got \(body)")
            return
        }
        #expect(items.count == 2)
    }

    @Test func aRunningCallHasNothingToPresent() {
        let tool = ToolCallInfo(id: "t1", name: "search-hybrid", arguments: "{}", result: nil, isError: false, isRunning: true)
        #expect(tool.presentation == nil)
    }
}

// MARK: - State

struct ToolResultSettleTests {
    @Test @MainActor func settlingAParsesTheResultOnce() throws {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.toolCall(ChatToolCallEvent(id: "t1", tool: "search-hybrid", arguments: "{\"query\":\"x\"}")))
        #expect(store.liveTurn?.tools.first?.presentation == nil)

        store.apply(.toolResult(ChatToolResultEvent(id: "t1", tool: "search-hybrid", result: Payload.search, isError: false)))
        let settled = try #require(store.liveTurn?.tools.first)
        guard case let .items(items, _)? = settled.presentation?.body else {
            Issue.record("no readable body after settle")
            return
        }
        #expect(items.count == 2)
        // The raw string is untouched: the presentation is derived, not a rewrite.
        #expect(settled.result == Payload.search)
    }
}

// MARK: - Layout

/// The point of the readable body is that it is bounded: a twenty-record answer
/// must not open as a wall, and the JSON must stay closed until asked for.
@MainActor
struct ToolResultBodyLayoutTests {
    private let width: CGFloat = 390

    @Test func aLongListOpensBounded() throws {
        let presentation = try #require(ToolResultPresentation.parse(Payload.hits(20)))
        let height = height(of: ToolResultBody(presentation: presentation))
        #expect(height > 20)
        #expect(height < 400)
    }

    @Test func aDocumentOpensBounded() throws {
        let presentation = try #require(ToolResultPresentation.parse(Payload.document))
        let height = height(of: ToolResultBody(presentation: presentation))
        // Eight lines of body, not twenty, and no raw block.
        #expect(height > 20)
        #expect(height < 400)
    }

    @Test func oneEndlessLineOpensBounded() throws {
        let presentation = try #require(ToolResultPresentation.parse(Payload.longLine))
        let height = height(of: ToolResultBody(presentation: presentation))
        #expect(height < 400)
    }

    @Test func theRawPayloadStaysClosed() throws {
        // A 400-record payload would dominate the transcript if it were shown
        // with the readable body.
        let presentation = try #require(ToolResultPresentation.parse(Payload.hits(400)))
        let height = height(of: ToolResultBody(presentation: presentation))
        #expect(height < 400)
    }

    private func height(of view: some View) -> CGFloat {
        let host = UIHostingController(rootView: view)
        _ = host.view
        let fitted = host.sizeThatFits(in: CGSize(width: width, height: .greatestFiniteMagnitude))
        #expect(
            fitted.height >= 0 && fitted.height < 10000,
            Comment(rawValue: "unexpected laid-out height \(fitted.height)")
        )
        return fitted.height
    }
}
