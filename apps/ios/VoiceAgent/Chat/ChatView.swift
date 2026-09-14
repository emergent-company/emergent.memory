import LiveKitComponents
import SwiftUI

/// The text chat: message list with rich markdown replies, per-turn live
/// activity (thinking block, tool-call chips, approval/question cards) rendered
/// right after the user message that started the turn, a typing indicator while
/// awaiting the first reply token, and a suggested-prompt empty state.
struct ChatView: View {
    @EnvironmentObject private var session: Session
    @EnvironmentObject private var controller: MemorySessionController

    /// Invoked when the user taps a suggested prompt (fills the composer).
    var onSuggestion: ((String) -> Void)?

    /// Suggested prompts shown in the empty chat state (web-chat parity).
    static let suggestedPrompts: [String] = [
        "What can you do?",
        "Summarize what we talked about",
        "What should I remember about you?",
        "What are you working on?",
    ]

    /// Scroll anchor pinned at the bottom of the message list.
    private static let bottomAnchorID = "chat.bottom"

    private var store: ChatActivityStore { controller.chatActivity }

    var body: some View {
        Group {
            if session.messages.isEmpty {
                emptyState()
            } else {
                messageList()
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

    @ViewBuilder
    private func userBubble(_ text: String) -> some View {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty {
            HStack {
                Spacer(minLength: 4 * .grid)
                Text(trimmed)
                    .font(.system(size: 17))
                    .foregroundStyle(.fg1)
                    .textSelection(.enabled)
                    .padding(.horizontal, 3 * .grid)
                    .padding(.vertical, 2 * .grid)
                    .background(
                        RoundedRectangle(cornerRadius: .cornerRadiusLarge)
                            .fill(.bg2)
                    )
                    .containerRelativeFrame(.horizontal, count: 5, span: 4, spacing: 0, alignment: .trailing)
            }
        }
    }

    @ViewBuilder
    private func agentBubble(_ text: String) -> some View {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty {
            HStack {
                MarkdownRenderer(text: trimmed)
                    .textSelection(.enabled)
                    .padding(.horizontal, 3 * .grid)
                    .padding(.vertical, 2 * .grid)
                    .background(
                        RoundedRectangle(cornerRadius: .cornerRadiusLarge)
                            .fill(.bg1)
                            .overlay(
                                RoundedRectangle(cornerRadius: .cornerRadiusLarge)
                                    .stroke(.separator1.opacity(0.5), lineWidth: 1)
                            )
                    )
                    .containerRelativeFrame(.horizontal, count: 5, span: 4, spacing: 0, alignment: .leading)
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
    @ViewBuilder
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

    // MARK: - Decisions

    private func decideApproval(_ request: ApprovalRequest, _ decision: ApprovalDecision) {
        guard !request.isAnswered else { return }
        store.submitApproval(questionId: request.questionId, decision: decision)
        let chatDecision: ChatDecision
        switch decision {
        case .approve:
            chatDecision = .approve(questionId: request.questionId)
        case let .reject(reason):
            chatDecision = .reject(questionId: request.questionId, reason: reason)
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
        VStack(spacing: 2 * .grid) {
            Spacer()
            Image(systemName: "bubble.left.and.bubble.right")
                .font(.system(size: 22, weight: .light))
            Text("Type a message")
                .font(.system(size: 15))
            Spacer()
            if onSuggestion != nil {
                SuggestedPromptsView(prompts: Self.suggestedPrompts) { prompt in
                    onSuggestion?(prompt)
                }
                .padding(.bottom, 2 * .grid)
            }
        }
        .foregroundStyle(.fg2)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding(.horizontal)
    }
}

/// Suggested-prompt chips for the empty chat state. Each chip fills the
/// composer (no auto-send) via `onPick`.
struct SuggestedPromptsView: View {
    let prompts: [String]
    let onPick: (String) -> Void

    var body: some View {
        VStack(spacing: 1 * .grid) {
            ForEach(prompts, id: \.self) { prompt in
                Button {
                    onPick(prompt)
                } label: {
                    Text(prompt)
                        .font(.system(size: 13, weight: .medium))
                        .foregroundStyle(.fg1)
                        .lineLimit(1)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 3 * .grid)
                        .padding(.vertical, 1 * .grid)
                        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
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
            return text
        }
    }
}
