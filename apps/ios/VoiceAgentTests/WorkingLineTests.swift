import Foundation
@testable import Memory
import SwiftUI
import Testing
import UIKit

// MARK: - State

struct TurnWorkingStateTests {
    private func turn(
        thinking: String = "",
        tools: [ToolCallInfo] = []
    ) -> ChatActivityStore.Turn {
        var turn = ChatActivityStore.Turn(anchorMessageID: "m1")
        turn.thinkingBlocks = thinking.isEmpty
            ? []
            : [ThinkingBlock(id: "r1", text: thinking, isLive: true)]
        turn.tools = tools
        return turn
    }

    private func tool(running: Bool) -> ToolCallInfo {
        ToolCallInfo(
            id: "t1",
            name: "entity-search",
            arguments: #"{"query":"retrieval"}"#,
            result: running ? nil : "ok",
            isError: false,
            isRunning: running
        )
    }

    @Test func showsWhileTheAgentHasNothingLiveToShow() {
        // The gap after send: no thinking, no steps.
        let state = TurnWorkingState.of(awaitingFirstReply: true, turn: turn())
        #expect(state == .thinking)
    }

    @Test func showsAfterTheStepsSettleAndTheReplyHasNotStarted() {
        // Three finished steps are on screen, nothing is moving: the line is
        // the only thing that says the turn is still alive.
        let state = TurnWorkingState.of(
            awaitingFirstReply: true,
            turn: turn(tools: [tool(running: false), tool(running: false)])
        )
        #expect(state == .thinking)
    }

    @Test func hidesWhileAStepRuns() {
        // The running row says what it is doing, with its own spinner.
        let state = TurnWorkingState.of(
            awaitingFirstReply: true,
            turn: turn(tools: [tool(running: false), tool(running: true)])
        )
        #expect(state == .hidden)
    }

    @Test func hidesWhileThinkingStreams() {
        // The thinking block's live header already says "Thinking".
        let state = TurnWorkingState.of(awaitingFirstReply: true, turn: turn(thinking: "Reading the question…"))
        #expect(state == .hidden)
    }

    @Test func hidesOnceTheReplyStarts() {
        let state = TurnWorkingState.of(awaitingFirstReply: false, turn: turn())
        #expect(state == .hidden)
    }

    @Test func hidesWithoutATurn() {
        #expect(TurnWorkingState.of(awaitingFirstReply: true, turn: nil) == .hidden)
    }

    @Test func hidesWhileTheTurnWaitsOnTheUser() {
        // A pending approval pauses the worker: the store stops awaiting, so
        // the line goes away rather than claiming work that is not happening.
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        #expect(store.isAwaitingFirstReply)
        store.apply(.approval(ChatApprovalEvent(questionId: "q1", tool: "document-delete", arguments: "{}")))
        #expect(!store.isAwaitingFirstReply)
        #expect(TurnWorkingState.of(awaitingFirstReply: store.isAwaitingFirstReply, turn: store.liveTurn) == .hidden)
    }
}

// MARK: - Turn start

struct TurnStartedAtTests {
    @Test @MainActor func aNewTurnIsStampedFromTheStoreClock() {
        let start = Date(timeIntervalSince1970: 1_700_000_000)
        let store = ChatActivityStore(now: { start })
        store.userTurnStarted(anchorMessageID: "m1")
        #expect(store.liveTurn?.startedAt == start)
    }

    @Test @MainActor func aTurnReopenedForTheSameMessageIsNotRestamped() {
        let start = Date(timeIntervalSince1970: 1_700_000_000)
        var clock = start
        let store = ChatActivityStore(now: { clock })
        store.userTurnStarted(anchorMessageID: "m1")
        clock = start.addingTimeInterval(30)
        store.userTurnStarted(anchorMessageID: "m1")
        // Idempotent: the same message does not open a second turn or reset
        // the clock the working line counts from.
        #expect(store.turns.count == 1)
        #expect(store.liveTurn?.startedAt == start)
    }
}

// MARK: - Duration

/// The line shows the rows' short form but speaks in words, so VoiceOver does
/// not read "12s" as "twelve ess".
struct WorkingLineDurationTests {
    private let english = Locale(identifier: "en_US")

    @Test func pairsTheShortFormWithItsSpokenForm() {
        let duration = WorkingLine.durationText(12, locale: english)
        #expect(duration?.text == "12s")
        #expect(duration?.spoken == "12 seconds")
    }

    @Test func speaksTheSameSpanAVoiceCouldSay() {
        let duration = WorkingLine.durationText(65, locale: english)
        #expect(duration?.text == "1m 5s")
        #expect(duration?.spoken == "1 minute, 5 seconds")
    }

    @Test func reportsNothingUnderASecond() {
        // Same floor as a step row: a fast turn shows the label, no time, and
        // no "0 seconds" for VoiceOver either.
        #expect(WorkingLine.durationText(0.4, locale: english) == nil)
    }
}

// MARK: - Settling

/// A turn leaves the pre-reply state only through the first token, so a stream
/// that never delivers one has to close it explicitly — otherwise the working
/// line spins on a turn that will never move again (`emergent.memory#445`).
@MainActor
struct TurnSettlingTests {
    @Test func aTurnWithoutAReplySettlesWithoutOne() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        #expect(store.isAwaitingFirstReply)

        store.settleWithoutReply()

        #expect(store.isAwaitingFirstReply == false)
        #expect(store.isGeneratingReply == false)
        #expect(TurnWorkingState.of(awaitingFirstReply: store.isAwaitingFirstReply, turn: store.liveTurn) == .hidden)
    }

    @Test func settlingKeepsTheTurnAndWhatItShowed() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.toolCall(ChatToolCallEvent(id: "t1", tool: "entity-search", arguments: "{}")))

        store.settleWithoutReply()

        #expect(store.liveTurn != nil)
        // The steps are still the turn's record of what happened.
        #expect(store.liveTurn?.tools.count == 1)
    }

    @Test func settlingIsIdempotent() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.settleWithoutReply()
        store.settleWithoutReply()
        #expect(store.isGeneratingReply == false)
        #expect(store.turns.count == 1)
    }

    @Test func settlingNeverClosesAReplyThatStarted() {
        // `markReplyFinished` owns a reply that streamed: settling it here would
        // cut a live answer short.
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.agentReplyStarted()
        store.settleWithoutReply()
        #expect(store.isGeneratingReply)
    }

    @Test func discardingATurnRemovesOnlyThatAnchor() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.settleWithoutReply()
        store.userTurnStarted(anchorMessageID: "m2")

        store.discardTurn(anchoredTo: "m1")

        #expect(store.turns.map(\.anchorMessageID) == ["m2"])
    }
}

// MARK: - Layout

/// The line is the replacement for a decorative animation, so it must be a
/// real, readable row — not a zero-height view that only looks right.
@MainActor
struct WorkingLineLayoutTests {
    private let width: CGFloat = 390

    @Test func theLineMeasuresAsARow() {
        let height = height(of: WorkingLine(state: .thinking, since: Date()))
        #expect(height > 10)
        #expect(height < 60)
    }

    @Test func aHiddenLineTakesNoSpace() {
        let height = height(of: WorkingLine(state: .hidden, since: Date()))
        #expect(height < 1)
    }

    @Test func aHiddenLineLeavesTheContentAroundItAlone() {
        // Hidden inside a stack, it must not open a gap: the turn's last line
        // sits as close to the content above as it would with no line at all.
        let neighbour = Text("the reply's last line")
        let without = height(of: VStack(alignment: .leading) { neighbour })
        let with = height(of: VStack(alignment: .leading) {
            neighbour
            WorkingLine(state: .hidden, since: Date())
        })
        #expect(with == without)
    }

    @Test func aVisibleLineAddsARow() {
        let neighbour = Text("the reply's last line")
        let without = height(of: VStack(alignment: .leading) { neighbour })
        let with = height(of: VStack(alignment: .leading) {
            neighbour
            WorkingLine(state: .thinking, since: Date())
        })
        #expect(with > without)
    }

    @Test func aTurnWithoutATimeStillDrawsTheLine() {
        let height = height(of: WorkingLine(state: .thinking, since: nil))
        #expect(height > 10)
        #expect(height < 60)
    }

    private func height(of view: some View) -> CGFloat {
        let host = UIHostingController(rootView: view)
        _ = host.view
        let fitted = host.sizeThatFits(in: CGSize(width: width, height: .greatestFiniteMagnitude))
        #expect(
            fitted.height >= 0 && fitted.height < 10000,
            Comment(rawValue: "unexpected laid-out height \(fitted.height)")
        )
        return fitted.height
    }
}
