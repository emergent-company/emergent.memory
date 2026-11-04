import LiveKit
import LiveKitComponents
import SwiftUI

// MARK: - Transcript

/// The text room's scroll: recorded history, recorded approvals, and the live
/// turn stitched into one timeline. Suggestions only appear for a brand-new
/// conversation with nothing in it yet.
struct ChatTranscriptView: View {
    let transcript: ChatSessionTranscriptStore
    let agentName: String
    let showsSuggestions: Bool
    let onSuggestion: (String) -> Void
    let onHistoryRetry: () -> Void
    let onHistoryApproval: (ApprovalRequest, ApprovalDecision) -> Void
    let onLiveApproval: (ApprovalRequest, ApprovalDecision) -> Void
    let onQuestion: (QuestionRequest, String) -> Void
    let onSurfaceAction: (ChatUISurface, ChatUIAction) -> Void

    @Environment(ChatActivityStore.self) private var activity

    private static let bottomAnchorID = "chat-session.bottom"

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 2 * .grid) {
                    historyStatus
                    historyEntries

                    // Approvals recorded before the live room opened.
                    historyApprovals
                    liveEntries

                    trailingActivity
                    surfaceCards()

                    Color.clear
                        .frame(height: 1)
                        .id(Self.bottomAnchorID)
                }
                .padding(.horizontal, 4 * .grid)
                .padding(.vertical, 2 * .grid)
            }
            .scrollDismissesKeyboard(.interactively)
            .onAppear { scrollToBottom(proxy, animated: false) }
            .onChange(of: transcript.history.count) { _, _ in scrollToBottom(proxy, animated: true) }
            .onChange(of: transcript.liveMessages) { _, _ in scrollToBottom(proxy, animated: true) }
            .onChange(of: activity.turns) { _, _ in scrollToBottom(proxy, animated: true) }
            .onChange(of: transcript.isLoadingHistory) { _, loading in
                if !loading {
                    scrollToBottom(proxy, animated: false)
                }
            }
            .overlay {
                if showsEmptyState {
                    ChatSessionEmptyState(
                        agentName: agentName,
                        showsSuggestions: showsSuggestions,
                        onSuggestion: onSuggestion
                    )
                }
            }
        }
    }

    @ViewBuilder
    private var historyStatus: some View {
        if let error = transcript.historyError {
            ChatHistoryErrorBanner(message: error, onRetry: onHistoryRetry)
        }
        if transcript.isLoadingHistory, transcript.history.isEmpty {
            LoadingStateView(title: "chats.history.loading", fillsHeight: false)
        }
    }

    private var historyEntries: some View {
        ForEach(Array(transcript.history.enumerated()), id: \.offset) { _, entry in
            historyEntry(entry)
        }
    }

    private var historyApprovals: some View {
        ForEach(transcript.pendingApprovals) { request in
            ApprovalCardView(request: request) { decision in
                onHistoryApproval(request, decision)
            }
        }
    }

    private var liveEntries: some View {
        ForEach(transcript.liveMessages) { message in
            ChatMessageBubble(message: message)
            anchoredActivity(for: message)
        }
    }

    private func scrollToBottom(_ proxy: ScrollViewProxy, animated: Bool) {
        if animated {
            withAnimation(.easeOut(duration: 0.25)) {
                proxy.scrollTo(Self.bottomAnchorID, anchor: .bottom)
            }
        } else {
            proxy.scrollTo(Self.bottomAnchorID, anchor: .bottom)
        }
    }

    @ViewBuilder
    private func historyEntry(_ entry: HistoryEntry) -> some View {
        switch entry {
        case let .message(message):
            ChatMessageBubble(message: message)
        case let .tools(tools):
            ToolCallsCard(tools: tools)
        }
    }

    private var showsEmptyState: Bool {
        guard transcript.historyError == nil, !transcript.isLoadingHistory,
              !activity.isAwaitingFirstReply
        else { return false }
        return transcript.history.isEmpty
            && transcript.liveMessages.isEmpty
            && transcript.pendingApprovals.isEmpty
            && activity.surfaces.isEmpty
            && !activity.hasVisibleActivity
    }

    @ViewBuilder
    private func anchoredActivity(for message: ChatMessage) -> some View {
        if message.role == .user,
           let turn = activity.turn(anchoredTo: message.id),
           turn.hasContent
        {
            ChatTurnActivityView(
                turn: turn,
                onApproval: { request, decision in onLiveApproval(request, decision) },
                onQuestion: { request, answer in onQuestion(request, answer) }
            )
        }
    }

    /// The current turn's activity when its anchor message is not in the live
    /// list (activity can arrive before the transcript), plus the working line
    /// while the agent has not answered yet.
    @ViewBuilder
    private var trailingActivity: some View {
        if let turn = activity.liveTurn, turn.hasContent,
           turn.anchorMessageID == nil || !transcript.liveMessages.contains(where: { $0.id == turn.anchorMessageID })
        {
            ChatTurnActivityView(
                turn: turn,
                onApproval: { request, decision in onLiveApproval(request, decision) },
                onQuestion: { request, answer in onQuestion(request, answer) }
            )
        }
        WorkingLine(
            state: TurnWorkingState.of(
                awaitingFirstReply: activity.isAwaitingFirstReply,
                turn: activity.liveTurn
            ),
            since: activity.liveTurn?.startedAt
        )
    }

    private func surfaceCards() -> some View {
        ForEach(activity.surfaces.filter(\.hasContent)) { surface in
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
}

// MARK: - Bubbles and live activity

/// One chat bubble, shared by recorded and live messages. Agent replies render
/// markdown; user messages stay plain text.
struct ChatMessageBubble: View {
    let message: ChatMessage

    var body: some View {
        let role: ConversationRole = message.role == .user ? .user : .assistant
        let text = message.text.trimmingCharacters(in: .whitespacesAndNewlines)
        if !text.isEmpty {
            HStack {
                if role.isUser {
                    Spacer(minLength: 4 * .grid)
                }
                VStack(alignment: role.horizontalAlignment, spacing: 1 * .grid) {
                    Text(role.labelKey)
                        .font(MemoryFont.meta)
                        .foregroundStyle(.fg3)
                        .padding(.horizontal, 1 * .grid)
                    Group {
                        if role.isUser {
                            Text(text)
                                .font(MemoryFont.conversationBody)
                                .foregroundStyle(.fg1)
                        } else {
                            MarkdownRenderer(text: text)
                        }
                    }
                    .textSelection(.enabled)
                    .padding(.horizontal, 3 * .grid)
                    .padding(.vertical, 2 * .grid)
                    .bubbleBackground(role, cornerRadius: .cornerRadiusMedium)
                }
                .containerRelativeFrame(
                    .horizontal,
                    count: 5,
                    span: 4,
                    spacing: 0,
                    alignment: role.frameAlignment
                )
                if !role.isUser {
                    Spacer(minLength: 4 * .grid)
                }
            }
            .id(message.id)
        }
    }
}

/// One live turn's activity: thinking, tool chips, and its interactive card.
struct ChatTurnActivityView: View {
    let turn: ChatActivityStore.Turn
    let onApproval: (ApprovalRequest, ApprovalDecision) -> Void
    let onQuestion: (QuestionRequest, String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            ForEach(turn.thinkingBlocks) { block in
                ThinkingBlockView(text: block.text, isLive: block.isLive)
            }
            ForEach(turn.toolGroups) { group in
                if ToolActivityGrouping.isGrouped(group.tools) {
                    ToolCallGroupView(group: group)
                } else {
                    ForEach(group.tools) { tool in
                        LiveToolChip(tool: tool)
                    }
                }
            }
            if let approval = turn.approval {
                ApprovalCardView(request: approval) { decision in
                    onApproval(approval, decision)
                }
            }
            if let question = turn.question {
                QuestionCardView(request: question) { answer in
                    onQuestion(question, answer)
                }
            }
        }
        .padding(.leading, 1 * .grid)
    }
}

// MARK: - Empty / error

/// Empty new chat: the agent's identity plus suggested prompts that fill the
/// composer (never send on tap).
struct ChatSessionEmptyState: View {
    let agentName: String
    let showsSuggestions: Bool
    let onSuggestion: (String) -> Void

    var body: some View {
        VStack(spacing: 3 * .grid) {
            Spacer()
            VStack(spacing: 2 * .grid) {
                ZStack {
                    Circle()
                        .fill(.bgAccent)
                    if let initial = agentName.first {
                        Text(String(initial).uppercased())
                            .font(MemoryFont.screenTitle)
                            .foregroundStyle(.fgAccent)
                    }
                }
                .frame(width: 16 * .grid, height: 16 * .grid)

                Text(agentName)
                    .font(MemoryFont.cardHeader)
                    .foregroundStyle(.fg0)
                    .lineLimit(1)
            }
            Text("chat.empty.title")
                .font(MemoryFont.bodySmall)
                .foregroundStyle(.fg2)
            Spacer()
            if showsSuggestions {
                SuggestedPromptsView(prompts: ChatView.suggestedPrompts) { prompt in
                    onSuggestion(prompt)
                }
                .padding(.bottom, 2 * .grid)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding(.horizontal)
    }
}

/// Non-fatal history failure: the live room still works, so this stays a banner
/// with a retry instead of taking over the screen.
struct ChatHistoryErrorBanner: View {
    let message: String
    let onRetry: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 2 * .grid) {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.caption)
                .foregroundStyle(.fgSerious)
            VStack(alignment: .leading, spacing: 0.5 * .grid) {
                Text("chats.history.error")
                    .font(MemoryFont.label)
                    .foregroundStyle(.fg1)
                Text(message)
                    .font(MemoryFont.meta)
                    .foregroundStyle(.fg3)
                    .lineLimit(2)
            }
            Spacer(minLength: 2 * .grid)
            Button("chats.history.retry") {
                onRetry()
            }
            .font(MemoryFont.labelEmphasis)
            .foregroundStyle(.fgAccent)
            .buttonStyle(.plain)
        }
        .padding(2 * .grid)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
    }
}
