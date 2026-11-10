import SwiftUI

// MARK: - Rows

/// One drawn row of the timeline. Consecutive tool calls fold into one activity
/// group at render time, so the group's identity is the first call id (#433)
/// rather than a position.
enum ChatTimelineRow: Identifiable {
    case item(TimelineItem)
    case tools(id: String, tools: [ToolCallInfo])

    var id: String {
        switch self {
        case let .item(item):
            item.id
        case let .tools(id, _):
            "tools-\(id)"
        }
    }
}

/// Folds the ordered items into drawn rows: every run of adjacent `.tool`
/// items becomes one ``ToolActivityGroup``; a non-tool row closes the run.
enum ChatTimelineRows {
    static func rows(from items: [TimelineItem]) -> [ChatTimelineRow] {
        var rows: [ChatTimelineRow] = []
        var run: [ToolCallInfo] = []

        func flushTools() {
            guard let first = run.first else { return }
            rows.append(.tools(id: first.id, tools: run))
            run.removeAll()
        }

        for item in items {
            if case let .tool(info) = item.kind {
                run.append(info)
                continue
            }
            flushTools()
            rows.append(.item(item))
        }
        flushTools()
        return rows
    }
}

// MARK: - Timeline

/// The one ordered chat timeline, shared by the text chat and the voice chat:
/// history and the live turn are the same list, walked in arrival order.
///
/// Tool runs fold into ``ToolActivityGroup`` here, reasoning rows draw with the
/// shared thinking block, and cards are the same views both sources use, so a
/// reloaded conversation looks like the live one instead of switching to a
/// different style with no thinking and no status.
struct ChatTimelineView: View {
    let store: ChatTimelineStore
    let onApproval: (ApprovalRequest, ApprovalDecision) -> Void
    let onQuestion: (QuestionRequest, String) -> Void
    let onSurfaceAction: (ChatUISurface, ChatUIAction) -> Void
    /// Recorded approvals are answered through the conversation endpoint, not
    /// the live room. Nil routes every card through `onApproval`.
    var onHistoryApproval: ((ApprovalRequest, ApprovalDecision) -> Void)?

    var body: some View {
        ForEach(ChatTimelineRows.rows(from: store.items)) { row in
            switch row {
            case let .item(item):
                itemRow(item)
            case let .tools(id, tools):
                ToolActivityGroup(tools: tools, turnKey: id)
            }
        }
    }

    @ViewBuilder
    private func itemRow(_ item: TimelineItem) -> some View {
        switch item.kind {
        case let .userMessage(text):
            ChatMessageBubble(message: ChatMessage(
                id: item.id,
                role: .user,
                text: text
            ))
        case let .assistantText(markdown, citations):
            ChatMessageBubble(message: ChatMessage(
                id: item.id,
                role: .assistant,
                text: markdown,
                citations: citations
            ))
        case let .reasoning(text, isLive, _):
            ThinkingBlockView(text: text, isLive: isLive)
        case .tool:
            // Folded into a ToolActivityGroup by the rows walker.
            EmptyView()
        case let .approval(request):
            ApprovalCardView(request: request) { decision in
                if store.isHistoryItem(item.id), let onHistoryApproval {
                    onHistoryApproval(request, decision)
                } else {
                    onApproval(request, decision)
                }
            }
        case let .question(request):
            QuestionCardView(request: request) { answer in
                onQuestion(request, answer)
            }
        case let .surface(surface):
            surfaceCards(surface)
        case .runBoundary:
            // Feeds the turn footer / status rows, not a transcript row.
            EmptyView()
        }
    }

    private func surfaceCards(_ surface: ChatUISurface) -> some View {
        ForEach(surface.components) { component in
            A2UIComponentCard(
                component: component,
                submittedResponse: surface.submittedActions[component.id]
            ) { action in
                onSurfaceAction(surface, action)
            }
        }
    }
}
