import Foundation
@testable import Memory
import SwiftUI
import Testing

// MARK: - Paseo-style styling + grouping views

struct ChatActivityViewTests {
    @MainActor
    @Test func stylingAndGroupingViewsConstruct() {
        // Message styling, thinking blocks, and both tool presentations must
        // stay constructible from the store's data shape.
        _ = ChatView()
        _ = ChatView(onSuggestion: { _ in })
        _ = ThinkingBlockView(text: "reasoning", isLive: true)
        _ = ThinkingBlockView(text: "reasoning", isLive: false)

        let first = ToolCallInfo(id: "t1", name: "web_search", arguments: "{}", result: "ok", isError: false)
        let second = ToolCallInfo(id: "t2", name: "read_file", arguments: "{}", result: nil, isError: false, isRunning: true)
        _ = LiveToolChip(tool: first)
        _ = ToolActivityGroup(tools: [first, second], turnID: UUID())
    }

    @MainActor
    @Test func subagentTrackConstructs() {
        _ = SubagentTrackView()
    }

    /// The plan card (#440) constructs both before and after a new chat learns
    /// its conversation id.
    @MainActor
    @Test func planCardConstructs() {
        let store = ChatPlanStore(client: FakeSessionTodoFetcher([.success([])]))
        _ = ChatPlanCard(store: store, conversationID: "conv-1")
        _ = ChatPlanCard(store: store, conversationID: nil)
    }
}
