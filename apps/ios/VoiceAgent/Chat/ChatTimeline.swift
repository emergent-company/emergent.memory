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
/// `id` is the server's identity when it sends one (tool call, run), and a
/// content-derived identity for messages, which carry none — never a bare
/// position for recorded rows, so a reload replaces them in place.
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

    /// The row's identity: the server id for tool calls and runs, and for
    /// messages — which the endpoint sends no id for — the created-at stamp
    /// plus the item's position in the response, so two stored rows cannot
    /// collide. Never a bare position, so a reload replaces rows in place.
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
    /// assert an exact duration, and read by both mappers (``ChatTimelineLive``
    /// stamps every live row with it).
    @ObservationIgnored let now: () -> Date
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

// MARK: - Mutation for the history and live mappers

/// The two mappers that feed this store live in their own files
/// (``ChatTimelineHistory``, ``ChatTimelineLive``) and Swift `private` is
/// file-scoped for a type member, so the rows are written through these
/// primitives rather than the storage. Readers still see `items`/`historyIDs`
/// directly; this file remains the only writer.
extension ChatTimelineStore {
    /// The newest row. Only it can still take a streaming delta.
    var lastItem: TimelineItem? {
        items.last
    }

    /// Appends one row in arrival order.
    func appendRow(_ item: TimelineItem) {
        items.append(item)
    }

    /// Replaces the kind of the newest row: a token, a settle, a shimmer stop.
    func replaceLastKind(_ kind: TimelineItem.Kind) {
        guard !items.isEmpty else { return }
        items[items.count - 1].kind = kind
    }

    /// Replaces the kind of the row at `index`: a card answer, a settle by id.
    func replaceKind(at index: Int, with kind: TimelineItem.Kind) {
        guard items.indices.contains(index) else { return }
        items[index].kind = kind
    }

    /// Replaces every row in one step and records which ids came from the
    /// record (a history reload).
    func replaceRows(_ rows: [TimelineItem], historyIDs recorded: Set<String>) {
        items = rows
        historyIDs = recorded
    }

    /// The run the live turn is streaming into (the last `meta` frame).
    var liveRun: String? {
        liveRunID
    }

    /// Starts a live run (the `meta` frame names it, when it names one); its
    /// rows carry this id so a reload can replace them.
    func beginLiveRun(_ runID: String?) {
        liveRunID = runID
    }

    /// Reserves a positional id for a new live assistant row. Positional is
    /// safe here: a reload removes live rows by run id, never by this id.
    func nextLiveAssistantID() -> String {
        liveAssistantCounter += 1
        return "live-assistant-\(liveAssistantCounter)"
    }
}
