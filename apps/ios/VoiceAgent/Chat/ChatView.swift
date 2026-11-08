import LiveKitComponents
import SwiftUI

/// The text chat: message list with rich markdown replies, per-turn live
/// activity (thinking block, tool-call chips, approval/question cards) rendered
/// right after the user message that started the turn, a working line while
/// awaiting the first reply token, and a suggested-prompt empty state.
struct ChatView: View {
    @EnvironmentObject private var session: Session
    @EnvironmentObject private var controller: MemorySessionController
    @Environment(AgentStore.self) private var agentStore
    @Environment(ChatActivityStore.self) private var store

    /// Invoked when the user taps a suggested prompt (fills the composer).
    var onSuggestion: ((String) -> Void)?

    /// Suggested prompts shown in the empty chat state (web-chat parity).
    /// Each carries a localized display key plus the English text sent to the
    /// agent when tapped, so the composer fill stays language-independent.
    static let suggestedPrompts: [SuggestedPrompt] = [
        SuggestedPrompt(key: "chat.suggestion.whatCanYouDo", text: "What can you do?"),
        SuggestedPrompt(key: "chat.suggestion.summarize", text: "Summarize what we talked about"),
        SuggestedPrompt(key: "chat.suggestion.remember", text: "What should I remember about you?"),
        SuggestedPrompt(key: "chat.suggestion.workingOn", text: "What are you working on?"),
    ]

    /// Scroll anchor pinned at the bottom of the message list.
    private static let bottomAnchorID = "chat.bottom"

    /// Which step runs the reader has opened or closed, kept here so it
    /// outlives the rows (they are rebuilt as steps settle).
    @State private var disclosure = ToolActivityDisclosure()

    var body: some View {
        Group {
            if session.messages.isEmpty {
                emptyState()
            } else {
                messageList()
            }
        }
        .environment(\.toolActivityDisclosure, disclosure)
        .onChange(of: store.turns.isEmpty) { _, empty in
            if empty {
                disclosure.clear()
            }
        }
    }

    // MARK: - Message list

    private func messageList() -> some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 2 * .grid) {
                    ForEach(session.messages) { item in
                        message(item)
                        // The turn this message started streams its activity
                        // in here, above the reply.
                        activity(for: item)
                    }

                    trailingActivity()
                    surfaceCards()

                    Color.clear
                        .frame(height: 1)
                        .id(Self.bottomAnchorID)
                }
                .padding(.vertical, 2 * .grid)
            }
            .scrollDismissesKeyboard(.interactively)
            // Open on the newest message; growth is the pin rule's job.
            .defaultScrollAnchor(.bottom, for: .initialOffset)
            // Follow the newest row while the reader is at the bottom, and never
            // while they are reading back (`emergent.memory#434`).
            .modifier(ChatAutoScroll(proxy: proxy, bottomAnchorID: Self.bottomAnchorID))
        }
        .padding(.horizontal)
        // A different controller means a different agent/session: give the
        // scroll view (and the offset it remembers) a fresh identity so a new
        // chat cannot open where the previous one was scrolled to.
        .id(ObjectIdentifier(controller))
    }

    // MARK: - Messages

    @ViewBuilder
    private func message(_ message: ReceivedMessage) -> some View {
        switch message.content {
        case let .userTranscript(text), let .userInput(text):
            userBubble(text)
        case let .agentTranscript(text):
            agentMessage(text)
        }
    }

    /// The user's message stays a right-aligned accent bubble. The visible role
    /// label is gone; the bubble's accessibility label keeps the speaker clear.
    @ViewBuilder
    private func userBubble(_ text: String) -> some View {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty {
            HStack {
                Spacer(minLength: 4 * .grid)
                Text(trimmed)
                    .font(MemoryFont.conversationBody)
                    .foregroundStyle(.fg1)
                    .textSelection(.enabled)
                    .padding(.horizontal, 3 * .grid)
                    .padding(.vertical, 2 * .grid)
                    .bubbleBackground(.user, cornerRadius: .cornerRadiusMedium)
                    .containerRelativeFrame(.horizontal, count: 5, span: 4, spacing: 0, alignment: .trailing)
                    .accessibilityLabel(Text(ConversationRole.user.labelKey) + Text(": ") + Text(trimmed))
            }
        }
    }

    /// Assistant replies render as plain leading-aligned Markdown: no bubble,
    /// no role label. The rendered markdown keeps its own VoiceOver structure.
    @ViewBuilder
    private func agentMessage(_ text: String) -> some View {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty {
            MarkdownRenderer(text: trimmed)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    // MARK: - Activity (tools / thinking / cards)

    /// The live/archived activity of the turn anchored to a user message.
    @ViewBuilder
    private func activity(for message: ReceivedMessage) -> some View {
        if let turn = store.turn(anchoredTo: message.id), turn.hasContent {
            turnActivity(turn)
        }
    }

    /// The still-live turn when it has no anchor message in the list yet
    /// (activity arriving before its user message is traced), plus the working
    /// line at the agent position.
    @ViewBuilder
    private func trailingActivity() -> some View {
        if let live = store.liveTurn, live.anchorMessageID == nil, live.hasContent {
            turnActivity(live)
        }
        // The working line shows only while the turn has no live row of its
        // own to speak for it.
        WorkingLine(
            state: TurnWorkingState.of(awaitingFirstReply: store.isAwaitingFirstReply, turn: store.liveTurn),
            since: store.liveTurn?.startedAt
        )
    }

    /// Renders one turn's accumulated activity: thinking blocks, contiguous
    /// tool-call runs, then the interactive card.
    private func turnActivity(_ turn: ChatActivityStore.Turn) -> some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            ForEach(turn.thinkingBlocks) { block in
                ThinkingBlockView(text: block.text, isLive: block.isLive)
            }
            ForEach(turn.toolGroups) { group in
                ToolActivityGroup(tools: group.tools, turnID: turn.id)
            }
            if let approval = turn.approval {
                ApprovalCardView(request: approval) { decision in
                    decideApproval(approval, decision)
                }
            }
            if let question = turn.question {
                QuestionCardView(request: question) { answer in
                    answerQuestion(question, answer: answer)
                }
            }
        }
        .padding(.leading, 1 * .grid)
    }

    // MARK: - A2UI surfaces

    /// Live structured-UI surfaces streamed by the agent, rendered as native
    /// SwiftUI cards. Each component's actions round-trip over `lk.chat.decision`.
    private func surfaceCards() -> some View {
        ForEach(store.surfaces.filter(\.hasContent)) { surface in
            ForEach(surface.components) { component in
                A2UIComponentCard(
                    component: component,
                    submittedResponse: surface.submittedActions[component.id]
                ) { action in
                    submitSurfaceAction(surface, action)
                }
            }
        }
    }

    private func submitSurfaceAction(_ surface: ChatUISurface, _ action: ChatUIAction) {
        store.submitSurfaceAction(
            surfaceId: surface.id,
            componentId: action.componentId,
            response: action.response
        )
        Task { await controller.sendDecision(.surfaceAction(surfaceId: surface.id, action: action)) }
    }

    // MARK: - Decisions

    private func decideApproval(_ request: ApprovalRequest, _ decision: ApprovalDecision) {
        guard !request.isAnswered else { return }
        store.submitApproval(questionId: request.questionId, decision: decision)
        let chatDecision: ChatDecision = switch decision {
        case .approve:
            .approve(questionId: request.questionId)
        case let .reject(reason):
            .reject(questionId: request.questionId, reason: reason)
        }
        Task { await controller.sendDecision(chatDecision) }
    }

    private func answerQuestion(_ request: QuestionRequest, answer: String) {
        guard !request.isAnswered else { return }
        store.submitQuestion(questionId: request.questionId, answer: answer)
        Task { await controller.sendDecision(.answer(questionId: request.questionId, value: answer)) }
    }

    // MARK: - Empty state

    private func emptyState() -> some View {
        VStack(spacing: 3 * .grid) {
            Spacer()
            agentIdentity()
            Text("chat.empty.title")
                .font(MemoryFont.bodySmall)
                .foregroundStyle(.fg2)
            Spacer()
            if onSuggestion != nil {
                SuggestedPromptsView(prompts: Self.suggestedPrompts) { prompt in
                    onSuggestion?(prompt)
                }
                .padding(.bottom, 2 * .grid)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding(.horizontal)
    }

    /// The selected agent's identity in the empty state: an initial avatar plus
    /// the agent name, so the blank chat is anchored to who the user is talking
    /// to. Falls back to a generic glyph when no agent is selected.
    @ViewBuilder
    private func agentIdentity() -> some View {
        let name = agentStore.selectedAgent?.name ?? ""
        VStack(spacing: 2 * .grid) {
            ZStack {
                Circle()
                    .fill(.bgAccent)
                if let initial = name.first {
                    Text(String(initial).uppercased())
                        .font(MemoryFont.screenTitle)
                        .foregroundStyle(.fgAccent)
                } else {
                    Image(systemName: "bubble.left.and.bubble.right")
                        .font(.title2.weight(.light))
                        .foregroundStyle(.fg3)
                }
            }
            .frame(width: 16 * .grid, height: 16 * .grid)

            if !name.isEmpty {
                Text(name)
                    .font(MemoryFont.cardHeader)
                    .foregroundStyle(.fg0)
                    .lineLimit(1)
            }
        }
    }
}

/// One suggested prompt: a localized display key plus the English text sent
/// to the agent when tapped (see ``ChatView/suggestedPrompts``).
struct SuggestedPrompt: Identifiable {
    let key: LocalizedStringKey
    let text: String
    var id: String {
        text
    }
}

/// Suggested-prompt chips for the empty chat state. Each chip fills the
/// composer (no auto-send) via `onPick`.
struct SuggestedPromptsView: View {
    let prompts: [SuggestedPrompt]
    let onPick: (String) -> Void

    var body: some View {
        VStack(spacing: 1 * .grid) {
            ForEach(prompts) { prompt in
                Button {
                    onPick(prompt.text)
                } label: {
                    Text(prompt.key)
                        .font(MemoryFont.label)
                        .foregroundStyle(.fg1)
                        .lineLimit(1)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 3 * .grid)
                        .padding(.vertical, 1 * .grid)
                        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
                        // Keep the pill's visual size while giving the row a
                        // full 44pt tap target.
                        .frame(minHeight: 44)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
        .frame(maxWidth: 64 * .grid)
    }
}

private extension ReceivedMessage.Content {
    /// The raw text of the message, regardless of its kind.
    var text: String {
        switch self {
        case let .agentTranscript(text),
             let .userTranscript(text),
             let .userInput(text):
            text
        }
    }
}
