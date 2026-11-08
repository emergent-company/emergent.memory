import Foundation
import Observation

// MARK: - Activity store

/// Accumulates the live rich-chat activity for the current conversation:
/// tool-call groups (correlated `tool_call`/`tool_result` pairs), contiguous
/// thinking blocks, pending approval/question cards, and live subagent runs.
///
/// Turns are anchored to the user `ReceivedMessage` that started them; the
/// chat renders a turn's activity right after that message, so live thinking /
/// tool chips stream in above the reply. Each new user message opens a fresh
/// turn; the previous turn is archived in place.
@MainActor
@Observable
final class ChatActivityStore {
    /// The turn model lives in its own file; expose the historical name so
    /// existing `ChatActivityStore.Turn` references keep working.
    typealias Turn = ChatTurn

    /// Supplies "now" for step timing. Injectable so a test can assert an
    /// exact duration instead of one measured against the wall clock.
    @ObservationIgnored private let now: () -> Date

    init(now: @escaping () -> Date = Date.init) {
        self.now = now
    }

    /// How long after the last agent token/activity the current reply is
    /// considered finished (stop affordance hidden). Never affects the
    /// "awaiting first reply" indicator.
    static let replyQuietInterval: TimeInterval = 3.0

    /// All turns in chronological order; the last one is the live turn.
    private(set) var turns: [Turn] = []

    /// Live A2UI surfaces (structured cards), in arrival order. Store-level
    /// rather than turn-bound: the server may update a surface across turns.
    private(set) var surfaces: [ChatUISurface] = []

    /// Replaces the surface list. The A2UI merge/action code lives in
    /// `ChatActivitySurfaces.swift`, which cannot write the `private(set)`
    /// property from another file.
    func replaceSurfaces(_ list: [ChatUISurface]) {
        surfaces = list
    }

    /// Live subagent runs, in first-seen order. Store-level: a run can outlive
    /// the turn that launched it.
    private(set) var subagents: [SubagentInfo] = []

    /// Mutation seam for the subagent upsert logic in
    /// `ChatActivitySubagents.swift` (a cross-file extension cannot write the
    /// `private(set)` storage directly).
    func mutateSubagents(_ body: (inout [SubagentInfo]) -> Void) {
        body(&subagents)
    }

    /// Quiet-timer driving `didReplyFinish` (only armed once a reply started).
    @ObservationIgnored private var quietTask: Task<Void, Never>?

    // MARK: - Current-turn state

    var liveTurn: Turn? {
        turns.last
    }

    /// True from a new user message until the first agent reply token arrives
    /// (drives the working line and the stop button pre-reply).
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
        surfaces = []
        subagents = []
    }

    // MARK: - Events

    /// Routes one decoded worker event into the live turn (or, for A2UI
    /// surfaces and subagents, into the store-level lists).
    func apply(_ event: ChatEvent) {
        if applyStoreLevelEvent(event) {
            return
        }
        guard var turn = liveTurn else { return }
        applyTurnEvent(event, to: &turn)
        turns[turns.count - 1] = turn
        armQuietFinishTimer()
    }

    /// Handles events that do not belong to one turn. Returns true when the
    /// event was consumed.
    private func applyStoreLevelEvent(_ event: ChatEvent) -> Bool {
        switch event {
        case let .ui(surface):
            closeLiveTurnInPlace()
            applySurface(surface)
            return true
        case let .subagent(subagent):
            upsertSubagent(subagent)
            return true
        default:
            return false
        }
    }

    private func applyTurnEvent(_ event: ChatEvent, to turn: inout Turn) {
        switch event {
        case let .toolCall(call):
            closeThinking(in: &turn)
            recordToolCall(call, in: &turn)
        case let .toolResult(result):
            closeThinking(in: &turn)
            settleToolResult(result, in: &turn)
        case let .thinking(id, delta):
            if !delta.isEmpty {
                appendThinking(id: id, delta: delta, in: &turn)
            }
        case let .approval(approval):
            closeThinking(in: &turn)
            turn.isToolGroupOpen = false
            turn.approval = ApprovalRequest(
                questionId: approval.questionId,
                tool: approval.tool,
                arguments: approval.arguments
            )
        case let .question(question):
            closeThinking(in: &turn)
            turn.isToolGroupOpen = false
            turn.question = QuestionRequest(
                questionId: question.questionId,
                question: question.question,
                interactionType: question.interactionType,
                options: question.options,
                placeholder: question.placeholder,
                maxLength: question.maxLength
            )
        case .ui, .subagent, .conversation:
            break // store-level or controller-level, not live turn activity
        }
    }

    /// Opens a live step, joining the open run when it is still consecutive.
    /// The start stamp is what lets the row report how long the work took — the
    /// worker's events carry no timing of their own.
    ///
    /// The gateway's category and subject travel with the event: the row then
    /// describes the call the way the web console's chip does, instead of
    /// classifying it from the app's own table.
    private func recordToolCall(_ call: ChatToolCallEvent, in turn: inout Turn) {
        let info = ToolCallInfo(
            id: call.id,
            name: call.tool,
            arguments: call.arguments,
            result: nil,
            isError: false,
            isRunning: true,
            startedAt: now(),
            category: call.category,
            subject: call.subject
        )
        if turn.isToolGroupOpen, let last = turn.toolGroups.indices.last {
            turn.toolGroups[last].tools.append(info)
        } else {
            turn.toolGroups.append(ToolCallGroup(id: call.id, tools: [info]))
        }
        turn.isToolGroupOpen = true
    }

    /// Settles a live step: keeps its start stamp, its category and its subject,
    /// records the result and the gateway's readable view of it, and measures
    /// the duration. A result with no matching call is ignored, and a call with
    /// no start stamp stays untimed rather than timed from now.
    private func settleToolResult(_ result: ChatToolResultEvent, in turn: inout Turn) {
        guard
            let groupIndex = turn.toolGroups.firstIndex(where: { group in
                group.tools.contains { $0.id == result.id }
            }),
            let toolIndex = turn.toolGroups[groupIndex].tools.firstIndex(where: { $0.id == result.id })
        else { return }
        let existing = turn.toolGroups[groupIndex].tools[toolIndex]
        turn.toolGroups[groupIndex].tools[toolIndex] = ToolCallInfo(
            id: result.id,
            name: existing.name,
            arguments: existing.arguments,
            result: result.result,
            isError: result.isError,
            isRunning: false,
            startedAt: existing.startedAt,
            duration: existing.startedAt.map { max(0, now().timeIntervalSince($0)) },
            responseSize: result.outputSize,
            category: result.category ?? existing.serverCategory,
            subject: existing.serverSubject,
            presentation: result.view?.presentation
        )
    }

    // MARK: - Thinking blocks

    /// Appends a delta to the active block, or opens one. A delta carrying a
    /// different worker id starts a new block; an id-less legacy delta always
    /// joins the active block. Thinking is a non-tool activity, so it closes
    /// the current tool group.
    private func appendThinking(id: String?, delta: String, in turn: inout Turn) {
        turn.isToolGroupOpen = false
        let blockID = id?.isEmpty == false ? id : nil

        if let last = turn.thinkingBlocks.indices.last, turn.thinkingBlocks[last].isLive {
            // Contiguity, not the worker segment id, defines a block: adjacent
            // reasoning segments merge even when they carry different ids.
            let active = turn.thinkingBlocks[last]
            turn.thinkingBlocks[last].text += (active.text.isEmpty ? "" : "\n") + delta
            return
        }
        turn.thinkingBlocks.append(ThinkingBlock(
            id: blockID ?? UUID().uuidString,
            text: delta,
            isLive: true
        ))
    }

    /// Closes the active thinking block, if one is still streaming.
    private func closeThinking(in turn: inout Turn) {
        guard let last = turn.thinkingBlocks.indices.last, turn.thinkingBlocks[last].isLive else { return }
        turn.thinkingBlocks[last].isLive = false
    }

    /// Closes the live turn's active thinking block and tool run. Used when a
    /// non-turn event (A2UI, new user message) ends the current activity.
    private func closeLiveTurnInPlace() {
        guard var turn = liveTurn else { return }
        closeThinking(in: &turn)
        turn.isToolGroupOpen = false
        turns[turns.count - 1] = turn
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

// MARK: - Message-driven transitions

extension ChatActivityStore {
    /// A new user message started a turn; anchor it to that message.
    func userTurnStarted(anchorMessageID: String) {
        // Idempotent: the same message must not open two turns.
        guard turn(anchoredTo: anchorMessageID) == nil else { return }
        quietTask?.cancel()
        quietTask = nil
        closeLiveTurnInPlace()
        turns.append(Turn(anchorMessageID: anchorMessageID, startedAt: now()))
    }

    /// The first agent reply token of the current turn arrived: dismiss the
    /// working line and arm the quiet-finish timer.
    func agentReplyStarted() {
        guard var turn = liveTurn else { return }
        // Every agent token is a non-thinking activity boundary, even after
        // the first one; a reasoning block that arrived mid-reply must close
        // when the answer resumes.
        closeThinking(in: &turn)
        turn.isToolGroupOpen = false
        if !turn.didReplyStart {
            turn.didReplyStart = true
            armQuietFinishTimer()
        }
        turns[turns.count - 1] = turn
    }

    /// Marks the current reply finished (called by the quiet timer; exposed so
    /// tests can drive the state machine deterministically).
    func markReplyFinished() {
        guard var turn = liveTurn, turn.didReplyStart, !turn.didReplyFinish else { return }
        closeThinking(in: &turn)
        turn.isToolGroupOpen = false
        turn.didReplyFinish = true
        turns[turns.count - 1] = turn
    }

    /// The user hit stop/interrupt: end the current generation window and hide
    /// both the working line and the stop button. The interrupt itself is
    /// sent by the controller (`lk.chat.interrupt`); this only flips state.
    func stopGenerating() {
        quietTask?.cancel()
        quietTask = nil
        guard var turn = liveTurn, !turn.didReplyFinish else { return }
        closeThinking(in: &turn)
        turn.isToolGroupOpen = false
        turn.didReplyStart = true
        turn.didReplyFinish = true
        turns[turns.count - 1] = turn
    }

    /// The turn ended before any reply token arrived: close it so the working
    /// line, the stop affordance and the composer's busy gate cannot stay live
    /// forever.
    ///
    /// A turn normally leaves the pre-reply state through ``agentReplyStarted``
    /// (the first token). A stream that ends, is refused, or yields nothing
    /// decodable never calls it, and ``markReplyFinished`` deliberately does
    /// nothing before a reply starts — so without this the app waits on a turn
    /// that will never move again. Whether the turn showed anything (a tool
    /// call, a card, a reasoning block) is the caller's call: this only ends the
    /// window.
    func settleWithoutReply() {
        guard var turn = liveTurn, !turn.didReplyStart, !turn.didReplyFinish else { return }
        quietTask?.cancel()
        quietTask = nil
        closeThinking(in: &turn)
        turn.isToolGroupOpen = false
        turn.didReplyStart = true
        turn.didReplyFinish = true
        turns[turns.count - 1] = turn
    }

    /// Drops the live turn anchored to `messageID`: its reply arrived from the
    /// record instead of the stream, so the live turn has nothing left to show.
    func discardTurn(anchoredTo messageID: String) {
        turns.removeAll { $0.anchorMessageID == messageID }
    }
}
