import Foundation

// MARK: - Interactive card models (shared with the card views)

/// An approval request waiting on the user, plus its submitted outcome
/// (`nil` until the user acts). Once a decision is recorded the card renders
/// in a non-interactive answered state.
struct ApprovalRequest: Identifiable, Equatable {
    let questionId: String
    let tool: String
    let arguments: String
    var decision: ApprovalDecision?

    var id: String { questionId }
    var isAnswered: Bool { decision != nil }
}

enum ApprovalDecision: Equatable {
    case approve
    case reject(reason: String?)
}

/// An `ask_user` question card, plus its submitted answer.
struct QuestionRequest: Identifiable, Equatable {
    let questionId: String
    let question: String
    let interactionType: QuestionInteractionType
    let options: [ChatQuestionOption]
    let placeholder: String?
    let maxLength: Int?
    var submittedAnswer: String?

    init(questionId: String, question: String, interactionType: QuestionInteractionType,
         options: [ChatQuestionOption], placeholder: String?, maxLength: Int?, submittedAnswer: String? = nil) {
        self.questionId = questionId
        self.question = question
        self.interactionType = interactionType
        self.options = options
        self.placeholder = placeholder
        self.maxLength = maxLength
        self.submittedAnswer = submittedAnswer
    }

    var id: String { questionId }
    var isAnswered: Bool { submittedAnswer != nil }
}

// MARK: - Activity store

/// Accumulates the live rich-chat activity for the current conversation:
/// tool-call chips (correlated `tool_call`/`tool_result` pairs), one
/// accumulated thinking block per turn, and pending approval/question cards.
///
/// Turns are anchored to the user `ReceivedMessage` that started them; the
/// chat renders a turn's activity right after that message, so live thinking /
/// tool chips stream in above the reply. Each new user message opens a fresh
/// turn; the previous turn is archived in place.
@MainActor
final class ChatActivityStore: ObservableObject {
    /// One user-initiated turn: everything the worker emitted for it.
    struct Turn: Identifiable, Equatable {
        let id: UUID
        /// The `ReceivedMessage.id` of the user message that started this turn.
        var anchorMessageID: String?
        /// Accumulated `thinking` deltas of this turn (one block).
        var thinkingText: String = ""
        /// Live/complete tool calls in arrival order.
        var tools: [ToolCallInfo] = []
        /// Pending (or answered) approval request.
        var approval: ApprovalRequest?
        /// Pending (or answered) question.
        var question: QuestionRequest?
        /// True once the first agent transcript of this turn has streamed in.
        var didReplyStart = false
        /// True once the reply is considered complete (quiet after the last
        /// agent token), hiding the stop affordance.
        var didReplyFinish = false

        init(id: UUID = UUID(), anchorMessageID: String?) {
            self.id = id
            self.anchorMessageID = anchorMessageID
        }

        /// Any content worth rendering (skips empty placeholder turns).
        var hasContent: Bool {
            !thinkingText.isEmpty || !tools.isEmpty || approval != nil || question != nil
        }

        /// The worker is paused waiting on the user (card shown).
        var isWaitingOnUser: Bool {
            if let approval, approval.decision == nil { return true }
            if let question, question.submittedAnswer == nil { return true }
            return false
        }
    }

    /// How long after the last agent token/activity the current reply is
    /// considered finished (stop affordance hidden). Never affects the
    /// "awaiting first reply" indicator.
    static let replyQuietInterval: TimeInterval = 3.0

    /// All turns in chronological order; the last one is the live turn.
    @Published private(set) var turns: [Turn] = []

    /// Quiet-timer driving `didReplyFinish` (only armed once a reply started).
    private var quietTask: Task<Void, Never>?

    // MARK: - Current-turn state

    var liveTurn: Turn? { turns.last }

    /// True from a new user message until the first agent reply token arrives
    /// (drives the typing indicator and the stop button pre-reply).
    var isAwaitingFirstReply: Bool {
        guard let turn = liveTurn else { return false }
        return !turn.didReplyStart && !turn.isWaitingOnUser
    }

    /// True while the worker is producing the current reply (drives the stop
    /// button). Cleared by the quiet timer after the reply finishes or when a
    /// card pauses the run.
    var isGeneratingReply: Bool {
        guard let turn = liveTurn else { return false }
        return !turn.didReplyFinish && !turn.isWaitingOnUser
    }

    var hasVisibleActivity: Bool {
        turns.contains(where: \.hasContent)
    }

    /// The turn anchored to `messageID`, if any.
    func turn(anchoredTo messageID: String) -> Turn? {
        turns.first { $0.anchorMessageID == messageID }
    }

    /// Clears all activity (new session).
    func reset() {
        quietTask?.cancel()
        quietTask = nil
        turns = []
    }

    // MARK: - Message-driven transitions

    /// A new user message started a turn; anchor it to that message.
    func userTurnStarted(anchorMessageID: String) {
        // Idempotent: the same message must not open two turns.
        guard turn(anchoredTo: anchorMessageID) == nil else { return }
        quietTask?.cancel()
        quietTask = nil
        turns.append(Turn(anchorMessageID: anchorMessageID))
    }

    /// The first agent reply token of the current turn arrived: dismiss the
    /// typing indicator and arm the quiet-finish timer.
    func agentReplyStarted() {
        guard var turn = liveTurn, !turn.didReplyStart else { return }
        turn.didReplyStart = true
        turns[turns.count - 1] = turn
        armQuietFinishTimer()
    }

    /// Marks the current reply finished (called by the quiet timer; exposed so
    /// tests can drive the state machine deterministically).
    func markReplyFinished() {
        guard var turn = liveTurn, turn.didReplyStart, !turn.didReplyFinish else { return }
        turn.didReplyFinish = true
        turns[turns.count - 1] = turn
    }

    /// The user hit stop/interrupt: end the current generation window and hide
    /// both the typing indicator and the stop button. The interrupt itself is
    /// sent by the controller (`lk.chat.interrupt`); this only flips state.
    func stopGenerating() {
        quietTask?.cancel()
        quietTask = nil
        guard var turn = liveTurn, !turn.didReplyFinish else { return }
        turn.didReplyStart = true
        turn.didReplyFinish = true
        turns[turns.count - 1] = turn
    }

    // MARK: - Events

    /// Routes one decoded worker event into the live turn.
    func apply(_ event: ChatEvent) {
        guard var turn = liveTurn else { return }
        switch event {
        case let .toolCall(call):
            turn.tools.append(ToolCallInfo(
                id: call.id,
                name: call.tool,
                arguments: call.arguments,
                result: nil,
                isError: false,
                isRunning: true
            ))
        case let .toolResult(result):
            if let index = turn.tools.firstIndex(where: { $0.id == result.id }) {
                turn.tools[index] = ToolCallInfo(
                    id: result.id,
                    name: turn.tools[index].name,
                    arguments: turn.tools[index].arguments,
                    result: result.result,
                    isError: result.isError,
                    isRunning: false
                )
            }
        case let .thinking(delta):
            if !delta.isEmpty {
                turn.thinkingText += (turn.thinkingText.isEmpty ? "" : "\n") + delta
            }
        case let .approval(approval):
            turn.approval = ApprovalRequest(
                questionId: approval.questionId,
                tool: approval.tool,
                arguments: approval.arguments
            )
        case let .question(question):
            turn.question = QuestionRequest(
                questionId: question.questionId,
                question: question.question,
                interactionType: question.interactionType,
                options: question.options,
                placeholder: question.placeholder,
                maxLength: question.maxLength
            )
        }
        turns[turns.count - 1] = turn
        armQuietFinishTimer()
    }

    // MARK: - Decisions (mark the card answered)

    func submitApproval(questionId: String, decision: ApprovalDecision) {
        guard var turn = liveTurn, var approval = turn.approval, approval.questionId == questionId else { return }
        approval.decision = decision
        turn.approval = approval
        turns[turns.count - 1] = turn
    }

    func submitQuestion(questionId: String, answer: String) {
        guard var turn = liveTurn, var question = turn.question, question.questionId == questionId else { return }
        question.submittedAnswer = answer
        turn.question = question
        turns[turns.count - 1] = turn
    }

    // MARK: - Quiet finish timer

    private func armQuietFinishTimer() {
        quietTask?.cancel()
        guard let turn = liveTurn, turn.didReplyStart, !turn.didReplyFinish else { return }
        quietTask = Task { [weak self] in
            do {
                try await Task.sleep(for: .seconds(Self.replyQuietInterval))
            } catch {
                return // cancelled — a new token/event re-arms.
            }
            guard !Task.isCancelled else { return }
            self?.markReplyFinished()
        }
    }
}
