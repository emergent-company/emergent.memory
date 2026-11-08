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

    var id: String {
        questionId
    }

    var isAnswered: Bool {
        decision != nil
    }
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
         options: [ChatQuestionOption], placeholder: String?, maxLength: Int?, submittedAnswer: String? = nil)
    {
        self.questionId = questionId
        self.question = question
        self.interactionType = interactionType
        self.options = options
        self.placeholder = placeholder
        self.maxLength = maxLength
        self.submittedAnswer = submittedAnswer
    }

    var id: String {
        questionId
    }

    var isAnswered: Bool {
        submittedAnswer != nil
    }
}

// MARK: - Live activity models

/// One contiguous reasoning segment. A block opens on the first thinking delta
/// and closes on the first non-thinking activity, so a turn can contain several
/// blocks (reason → tool → reason).
///
/// `isLive` is the spinner only; `isOpen` is "a later delta may still append
/// here". A `done: true` frame stops the spinner but leaves the block open, so
/// the next delta before an activity boundary joins the same row (the web
/// console's shimmer semantics). Only an activity boundary closes it.
struct ThinkingBlock: Identifiable, Equatable {
    let id: String
    var text: String
    var isLive: Bool
    var isOpen: Bool
}

/// A run of consecutive tool calls. A non-tool activity closes the run, so the
/// next tool call starts a new group.
struct ToolCallGroup: Identifiable, Equatable {
    let id: String
    var tools: [ToolCallInfo]
}

/// The lifecycle state of a live subagent run. Unknown wire values degrade to
/// `.unknown` rather than dropping the whole event.
enum SubagentStatus: String, Equatable, Sendable {
    case queued
    case running
    case completed
    case failed
    case paused
    case canceled
    case unknown

    init(wireValue: String?) {
        guard let wireValue, let status = Self(rawValue: wireValue) else {
            self = .unknown
            return
        }
        self = status
    }

    /// Queued or running: the run is actively working.
    var isWorking: Bool {
        self == .queued || self == .running
    }

    /// Paused: the run is waiting on user input.
    var needsInput: Bool {
        self == .paused
    }

    var hasFailed: Bool {
        self == .failed
    }
}

/// A live subagent run shown by the composer-track pill.
struct SubagentInfo: Identifiable, Equatable {
    let id: String
    var parentRunId: String?
    var runId: String?
    var agentName: String?
    var task: String?
    var status: SubagentStatus
    var error: String?
}

/// One user-initiated turn: everything the worker emitted for it. Store-side
/// `ChatActivityStore` owns a chronologically ordered list of these.
struct ChatTurn: Identifiable, Equatable {
    let id: UUID
    /// The `ReceivedMessage.id` of the user message that started this turn.
    var anchorMessageID: String?
    /// Contiguous thinking blocks in arrival order.
    var thinkingBlocks: [ThinkingBlock] = []
    /// Consecutive tool-call runs. A non-tool activity closes the run.
    var toolGroups: [ToolCallGroup] = []
    /// Pending (or answered) approval request.
    var approval: ApprovalRequest?
    /// Pending (or answered) question.
    var question: QuestionRequest?
    /// True once the first agent transcript of this turn has streamed in.
    var didReplyStart = false
    /// True once the reply is considered complete (quiet after the last
    /// agent token), hiding the stop affordance.
    var didReplyFinish = false
    /// When the user's message opened this turn. Nil for a turn assembled
    /// without a clock; the working line then shows no timer.
    var startedAt: Date?
    /// Whether the most recent tool call may still join the last group.
    /// Closed by any non-tool activity.
    var isToolGroupOpen = false

    init(id: UUID = UUID(), anchorMessageID: String?, startedAt: Date? = nil) {
        self.id = id
        self.anchorMessageID = anchorMessageID
        self.startedAt = startedAt
    }

    /// Every tool call in arrival order, flattened across groups. Kept for
    /// consumers (working line, tests) that only need the flat list.
    var tools: [ToolCallInfo] {
        get { toolGroups.flatMap(\.tools) }
        set {
            toolGroups = newValue.isEmpty
                ? []
                : [ToolCallGroup(id: newValue[0].id, tools: newValue)]
            isToolGroupOpen = false
        }
    }

    /// Any content worth rendering (skips empty placeholder turns).
    var hasContent: Bool {
        !thinkingBlocks.isEmpty || !tools.isEmpty || approval != nil || question != nil
    }

    /// The worker is paused waiting on the user (card shown).
    var isWaitingOnUser: Bool {
        if let approval, approval.decision == nil {
            return true
        }
        if let question, question.submittedAnswer == nil {
            return true
        }
        return false
    }
}
