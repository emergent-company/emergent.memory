import Foundation
import Observation

// MARK: - Model

/// One run of the agent: the `run_start`/`run_end` pair the history endpoint
/// records. Not drawn by default; it feeds the turn footer and status rows.
struct RunWindow: Equatable {
    /// The run id both boundary items carry.
    let id: String
    /// Model that produced the run, when the record names one.
    var model: String?
    /// `running`, `completed`, `failed`, … as the server recorded it.
    var status: String?
    /// When the run started.
    var startedAt: Date?
    /// How long it ran, measured across its two boundary items.
    var duration: TimeInterval?
    /// The failure reason of a failed run.
    var error: String?
}

/// One row of the ordered chat timeline: history and the live turn share one
/// list, in arrival order.
///
/// `id` is the server's identity when there is one (message id, tool call id,
/// run id), never a position, so a reload replaces items in place instead of
/// renumbering them.
struct TimelineItem: Identifiable, Equatable {
    enum Kind: Equatable {
        case userMessage(text: String)
        case assistantText(markdown: String, citations: [ChatCitation])
        /// A reasoning segment. `isLive` is the streaming shimmer, `isOpen`
        /// says a later delta may still append here.
        case reasoning(text: String, isLive: Bool, isOpen: Bool)
        case tool(ToolCallInfo)
        case approval(ApprovalRequest)
        case question(QuestionRequest)
        case surface(ChatUISurface)
        case runBoundary(RunWindow)
    }

    /// Server id when known: message id, tool call id, run id.
    let id: String
    let runID: String?
    let createdAt: Date?
    var kind: Kind
}

// MARK: - Store

/// The one ordered list of chat rows, fed by two inputs: recorded history
/// (`GET /api/conversations/:id/history`, mapped by ``ChatTimelineHistory``)
/// and the live stream (`ChatStreamEvent` from the HTTP transport, or the
/// worker's `ChatEvent` on the voice path — both applied in `ChatTimelineLive`).
///
/// Rows keep arrival order, so a turn that went think → tool → text → tool →
/// text renders in that order instead of thinking-then-tools-then-all-text.
/// A history reload replaces the rows of the runs it now contains and leaves
/// the live rows that are still newer than the record, so nothing renders twice
/// and no activity is orphaned below the reply.
@MainActor
@Observable
final class ChatTimelineStore {
    /// Every row, in arrival order.
    private(set) var items: [TimelineItem] = []
    private(set) var isLoadingHistory = false
    private(set) var historyError: String?
    /// Ids that came from the record, so the view can route a card's answer
    /// through the conversation endpoint instead of the live room.
    private(set) var historyIDs: Set<String> = []

    /// Clock for row timestamps and tool durations. Injectable so a test can
    /// assert an exact duration.
    @ObservationIgnored private let now: () -> Date
    /// Run id the live turn is streaming into (`meta` frame).
    @ObservationIgnored private var liveRunID: String?
    /// Counts live assistant rows, so a reply that restarts after a tool call
    /// gets its own id.
    @ObservationIgnored private var liveAssistantCounter = 0

    init(now: @escaping () -> Date = { Date() }) {
        self.now = now
    }

    // MARK: - History loading state

    func beginHistoryLoad() {
        isLoadingHistory = true
        historyError = nil
    }

    func historyLoadFailed(_ message: String) {
        isLoadingHistory = false
        historyError = message
    }

    func cancelHistoryLoad() {
        isLoadingHistory = false
        historyError = nil
    }

    func reset() {
        items = []
        historyIDs = []
        historyError = nil
        isLoadingHistory = false
        liveRunID = nil
        liveAssistantCounter = 0
    }

    /// Whether `id` came from the record (its card is answered over HTTP).
    func isHistoryItem(_ id: String) -> Bool {
        historyIDs.contains(id)
    }

    /// Whether the timeline has any row at all (drives the empty state).
    var isEmpty: Bool {
        items.isEmpty
    }

    // MARK: - Cards

    func submitApproval(questionId: String, decision: ApprovalDecision) {
        guard let index = items.lastIndex(where: { item in
            if case let .approval(request) = item.kind {
                return request.questionId == questionId
            }
            return false
        }), case var .approval(request) = items[index].kind
        else { return }
        request.decision = decision
        items[index].kind = .approval(request)
    }

    func submitQuestion(questionId: String, answer: String) {
        guard let index = items.lastIndex(where: { item in
            if case let .question(request) = item.kind {
                return request.questionId == questionId
            }
            return false
        }), case var .question(request) = items[index].kind
        else { return }
        request.submittedAnswer = answer
        items[index].kind = .question(request)
    }
}
