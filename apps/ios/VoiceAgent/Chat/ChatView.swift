import LiveKitComponents
import SwiftUI

/// The text chat: message list with rich markdown replies, per-turn live
/// activity (thinking block, tool-call chips, approval/question cards) rendered
/// right after the user message that started the turn, a typing indicator while
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

    var body: some View {
        if session.messages.isEmpty {
            emptyState()
        } else {
            messageList()
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
            .onAppear {
                scrollToBottom(proxy, animated: false)
            }
            .onChange(of: session.messages.count) { _, _ in
                scrollToBottom(proxy, animated: true)
            }
            .onChange(of: session.messages.last?.content.text) { _, _ in
                // Streaming transcript grew.
                scrollToBottom(proxy, animated: false)
            }
            .onChange(of: store.turns) { _, _ in
                // Activity events (thinking delta, new chip, card) arrived.
                scrollToBottom(proxy, animated: true)
            }
            .onChange(of: store.isAwaitingFirstReply) { _, awaiting in
                if awaiting {
                    scrollToBottom(proxy, animated: true)
                }
            }
        }
        .padding(.horizontal)
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

    // MARK: - Messages

    @ViewBuilder
    private func message(_ message: ReceivedMessage) -> some View {
        switch message.content {
        case let .userTranscript(text), let .userInput(text):
            userBubble(text)
        case let .agentTranscript(text):
            agentBubble(text)
        }
    }

    /// A subtle role label above a bubble, so speaker attribution is
    /// unambiguous on both the accent-tinted user bubble and the raised agent
    /// bubble. Shared copy with the session timeline.
    private func roleLabel(_ role: ConversationRole) -> some View {
        Text(role.labelKey)
            .font(MemoryFont.meta)
            .foregroundStyle(.fg3)
            .padding(.horizontal, 1 * .grid)
    }

    @ViewBuilder
    private func userBubble(_ text: String) -> some View {
        let role = ConversationRole.user
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty {
            HStack {
                Spacer(minLength: 4 * .grid)
                VStack(alignment: role.horizontalAlignment, spacing: 1 * .grid) {
                    roleLabel(role)
                    Text(trimmed)
                        .font(MemoryFont.conversationBody)
                        .foregroundStyle(.fg1)
                        .textSelection(.enabled)
                        .padding(.horizontal, 3 * .grid)
                        .padding(.vertical, 2 * .grid)
                        .bubbleBackground(role, cornerRadius: .cornerRadiusMedium)
                }
                .containerRelativeFrame(.horizontal, count: 5, span: 4, spacing: 0, alignment: role.frameAlignment)
            }
        }
    }

    @ViewBuilder
    private func agentBubble(_ text: String) -> some View {
        let role = ConversationRole.assistant
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty {
            HStack {
                VStack(alignment: role.horizontalAlignment, spacing: 1 * .grid) {
                    roleLabel(role)
                    MarkdownRenderer(text: trimmed)
                        .textSelection(.enabled)
                        .padding(.horizontal, 3 * .grid)
                        .padding(.vertical, 2 * .grid)
                        .bubbleBackground(role, cornerRadius: .cornerRadiusMedium)
                }
                .containerRelativeFrame(.horizontal, count: 5, span: 4, spacing: 0, alignment: role.frameAlignment)
                Spacer(minLength: 4 * .grid)
            }
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
    /// (activity arriving before its user message is traced), plus the typing
    /// indicator at the agent position.
    @ViewBuilder
    private func trailingActivity() -> some View {
        if let live = store.liveTurn, live.anchorMessageID == nil, live.hasContent {
            turnActivity(live)
        }
        if store.isAwaitingFirstReply {
            HStack {
                TypingIndicatorView()
                Spacer(minLength: 4 * .grid)
            }
            .padding(.top, 1 * .grid)
        }
    }

    /// Renders one turn's accumulated activity: thinking block, tool chips,
    /// then the interactive card.
    private func turnActivity(_ turn: ChatActivityStore.Turn) -> some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            if !turn.thinkingText.isEmpty {
                ThinkingBlockView(text: turn.thinkingText, isLive: !turn.didReplyStart)
            }
            if !turn.tools.isEmpty {
                VStack(alignment: .leading, spacing: 1 * .grid) {
                    ForEach(turn.tools) { tool in
                        LiveToolChip(tool: tool)
                    }
                }
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
