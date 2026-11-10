import Foundation
@testable import Memory
import Testing

/// The one ordered chat timeline: history and the live stream must produce the
/// same rows in the same order, and a reload must replace the turn it now holds
/// instead of stacking a second copy under the reply (`emergent.memory#435`).
///
/// These are static checks: there is no Swift toolchain on this host, so the
/// suite was written and reviewed but not run locally (see the PR body).
@MainActor
struct ChatTimelineTests {
    // MARK: - 1. History conversion

    @Test func historyRendersThinkToolTextToolTextInArrivalOrder() throws {
        let items = try Fixture.recorded()

        #expect(Fixture.labels(items) == [
            "user", "run", "reasoning", "tool", "assistant", "tool", "assistant", "run",
        ])
        #expect(Fixture.reasoningTexts(items) == ["thinking one"])
        #expect(Fixture.assistantTexts(items) == ["Let me check…", "Here is the answer."])
        #expect(Fixture.toolIDs(items) == ["t1", "t2"])
        // The tools keep their recorded facts, not a re-derived summary.
        let firstTool = try #require(items.first { $0.id == "t1" })
        guard case let .tool(info) = firstTool.kind else {
            Issue.record("t1 is not a tool row")
            return
        }
        #expect(info.name == "entity_search")
        #expect(info.isRunning == false)
        #expect(info.isError == false)
        #expect(info.duration == 1.2)
    }

    @Test func runBoundariesCarryTheRunFactsWithoutBecomingRows() throws {
        let boundaries = try Fixture.recorded().compactMap { item -> RunWindow? in
            guard case let .runBoundary(window) = item.kind else { return nil }
            return window
        }

        #expect(boundaries.count == 2)
        #expect(boundaries.first?.id == "r1")
        #expect(boundaries.last?.status == "completed")
        // Measured across the two boundary items.
        #expect(boundaries.last?.duration == 10)
    }

    @Test func answeredAskUserBecomesAQuestionCardWithItsAnswer() throws {
        let json = """
        {"items": [
          {"kind": "tool_call", "id": "q1", "run_id": "r1", "tool_name": "ask_user",
           "tool_input": {"question": "Which one?", "interaction_type": "buttons",
                          "options": [{"label": "A", "value": "a"}]},
           "tool_output": {"response": "a"}, "tool_status": "completed"}
        ]}
        """
        let history = try JSONDecoder().decode(ConversationHistory.self, from: Data(json.utf8))
        let items = ChatTimelineHistory.items(from: history)

        #expect(Fixture.labels(items) == ["question"])
        guard case let .question(request) = try #require(items.first?.kind) else {
            Issue.record("answered ask_user is not a question row")
            return
        }
        #expect(request.question == "Which one?")
        #expect(request.options.map(\.value) == ["a"])
        #expect(request.submittedAnswer == "a")
        #expect(request.isAnswered)
    }

    // MARK: - 2. Reload without duplication

    @Test func reloadAfterALiveTurnReplacesTheTurnInsteadOfStackingIt() throws {
        let store = ChatTimelineStore()
        store.apply(.meta(conversationID: "conv-1", runID: "r1"))
        store.appendUserMessage(id: "u-live", text: "hi")
        store.apply(.activity(.thinking(id: "live-think", delta: "thinking one")))
        store.apply(.activity(.toolCall(ChatToolCallEvent(id: "t1", tool: "entity_search", arguments: "{}"))))
        store.apply(.text("Let me check"))

        let recorded = try Fixture.recorded()
        store.replaceHistory(recorded)

        // The recorded turn is the whole list: the optimistic user message, the
        // partial reply and the live tool row were superseded, not appended.
        #expect(store.items.map(\.id) == recorded.map(\.id))
        #expect(Fixture.labels(store.items) == Fixture.labels(recorded))
        #expect(Fixture.assistantTexts(store.items) == ["Let me check…", "Here is the answer."])
        #expect(store.items.filter { $0.id == "t1" }.count == 1)
        #expect(Fixture.reasoningTexts(store.items) == ["thinking one"])
    }

    @Test func openReasoningStaysOpenAcrossAReload() throws {
        let store = ChatTimelineStore()
        store.apply(.meta(conversationID: "conv-1", runID: "r1"))
        store.apply(.activity(.thinking(id: "live-think", delta: "thinking one")))

        let recorded = try Fixture.recorded()
        store.replaceHistory(recorded)

        let reasoning = try #require(store.items.first { item in
            if case .reasoning = item.kind {
                return true
            }
            return false
        })
        #expect(reasoning.kind == .reasoning(text: "thinking one", isLive: false, isOpen: true))
    }

    @Test func reloadKeepsARowTheRecordDoesNotContainYet() throws {
        let store = ChatTimelineStore()
        store.apply(.meta(conversationID: "conv-1", runID: "r2"))
        store.apply(.text("A newer turn the record has not written yet."))

        let recorded = try Fixture.recorded()
        store.replaceHistory(recorded)

        // Run r2 is not in the record, so its row stays: the newest turn is on
        // screen while the record catches up.
        #expect(Fixture.assistantTexts(store.items).last == "A newer turn the record has not written yet.")
    }
}

// MARK: - Fixtures

/// One recorded turn (think → tool → text → tool → text) and the small readers
/// each assertion uses. Shared with the live-parity suite.
@MainActor
enum Fixture {
    static let historyJSON = """
    {
      "conversation_id": "conv-1",
      "items": [
        {"kind": "message", "id": "m0", "role": "user", "content": {"text": "hi"}},
        {"kind": "run_start", "run_id": "r1", "run_status": "running",
         "run_model": "test-model", "created_at": "2026-01-01T00:00:00Z"},
        {"kind": "message", "id": "m1", "run_id": "r1", "step_number": 1, "role": "assistant",
         "content": {"text": "", "reasoning": "thinking one"},
         "created_at": "2026-01-01T00:00:01Z"},
        {"kind": "tool_call", "id": "t1", "run_id": "r1", "step_number": 2,
         "tool_name": "entity_search", "tool_input": {"query": "a"}, "tool_output": {"items": []},
         "tool_status": "completed", "duration_ms": 1200,
         "created_at": "2026-01-01T00:00:02Z"},
        {"kind": "message", "id": "m2", "run_id": "r1", "step_number": 3, "role": "assistant",
         "content": {"text": "Let me check…"}, "created_at": "2026-01-01T00:00:03Z"},
        {"kind": "tool_call", "id": "t2", "run_id": "r1", "step_number": 4,
         "tool_name": "entity_search", "tool_input": {"query": "b"}, "tool_output": {"items": []},
         "tool_status": "completed", "duration_ms": 800,
         "created_at": "2026-01-01T00:00:04Z"},
        {"kind": "message", "id": "m3", "run_id": "r1", "step_number": 5, "role": "assistant",
         "content": {"text": "Here is the answer."}, "created_at": "2026-01-01T00:00:05Z"},
        {"kind": "run_end", "run_id": "r1", "run_status": "completed",
         "created_at": "2026-01-01T00:00:10Z"}
      ],
      "pending_approvals": []
    }
    """

    static func history() throws -> ConversationHistory {
        try JSONDecoder().decode(ConversationHistory.self, from: Data(historyJSON.utf8))
    }

    static func recorded() throws -> [TimelineItem] {
        try ChatTimelineHistory.items(from: history())
    }

    static func labels(_ items: [TimelineItem]) -> [String] {
        items.map { item in
            switch item.kind {
            case .userMessage: "user"
            case .assistantText: "assistant"
            case .reasoning: "reasoning"
            case .tool: "tool"
            case .approval: "approval"
            case .question: "question"
            case .surface: "surface"
            case .runBoundary: "run"
            }
        }
    }

    /// The rows a chat actually draws: run boundaries feed the footer, not the
    /// transcript, so parity is compared without them.
    static func drawn(_ items: [TimelineItem]) -> [TimelineItem] {
        items.filter { item in
            if case .runBoundary = item.kind {
                return false
            }
            return true
        }
    }

    static func assistantTexts(_ items: [TimelineItem]) -> [String] {
        items.compactMap { item in
            guard case let .assistantText(markdown, _) = item.kind else { return nil }
            return markdown
        }
    }

    static func reasoningTexts(_ items: [TimelineItem]) -> [String] {
        items.compactMap { item in
            guard case let .reasoning(text, _, _) = item.kind else { return nil }
            return text
        }
    }

    static func toolIDs(_ items: [TimelineItem]) -> [String] {
        items.compactMap { item in
            guard case let .tool(info) = item.kind else { return nil }
            return info.id
        }
    }

    static func record(_ text: String) -> SessionRecord {
        SessionRecord(kind: "turn", role: "user", text: text)
    }

    static func tool(_ id: String) -> ToolCallInfo {
        ToolCallInfo(id: id, name: "entity_search", arguments: "{}", result: "{}", isError: false)
    }
}
