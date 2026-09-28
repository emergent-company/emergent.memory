import Foundation
@testable import Memory
import SwiftUI
import Testing

// MARK: - 3.2 / 3.4 — Activity store: correlation + accumulation

struct ChatActivityStoreTests {
    @MainActor
    @Test func correlatesToolCallsAndResults() throws {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.toolCall(ChatToolCallEvent(id: "t1", tool: "web_search", arguments: "{}")))
        store.apply(.toolCall(ChatToolCallEvent(id: "t2", tool: "read_file", arguments: "{\"p\":\"/x\"}")))

        guard let turn = store.liveTurn else {
            Issue.record("expected a live turn")
            return
        }
        let toolIDs = turn.tools.map(\.id)
        let allRunning = turn.tools.allSatisfy(\.isRunning)
        #expect(toolIDs == ["t1", "t2"])
        #expect(allRunning)

        // Result for t2 arrives first; t1 still running.
        store.apply(.toolResult(ChatToolResultEvent(id: "t2", tool: "read_file", result: "file body", isError: false)))
        guard let afterT2 = store.liveTurn else {
            Issue.record("expected a live turn")
            return
        }
        try #require(afterT2.tools.count == 2)
        #expect(afterT2.tools[1].result == "file body")
        #expect(afterT2.tools[1].isRunning == false)
        #expect(afterT2.tools[0].isRunning == true)

        store.apply(.toolResult(ChatToolResultEvent(id: "t1", tool: "web_search", result: "boom", isError: true)))
        guard let finalTurn = store.liveTurn else {
            Issue.record("expected a live turn")
            return
        }
        try #require(finalTurn.tools.count == 2)
        #expect(finalTurn.tools[0].isError == true)
        #expect(finalTurn.tools[0].result == "boom")
        #expect(finalTurn.tools[0].isRunning == false)
    }

    @MainActor
    @Test func accumulatesThinkingDeltasIntoOneBlock() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.thinking(delta: "first"))
        store.apply(.thinking(delta: "second"))
        store.apply(.thinking(delta: "third"))

        guard let turn = store.liveTurn else {
            Issue.record("expected a live turn")
            return
        }
        #expect(turn.thinkingText == "first\nsecond\nthird")
    }

    @MainActor
    @Test func routesApprovalAndQuestionIntoLiveTurn() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.approval(ChatApprovalEvent(questionId: "q1", tool: "delete_file", arguments: "{}")))
        store.apply(.question(ChatQuestionEvent(
            questionId: "q2",
            question: "Which one?",
            interactionType: .buttons,
            options: [ChatQuestionOption(label: "A", value: "a", description: nil)],
            placeholder: nil,
            maxLength: nil
        )))

        guard let turn = store.liveTurn else {
            Issue.record("expected a live turn")
            return
        }
        #expect(turn.approval?.questionId == "q1")
        #expect(turn.approval?.isAnswered == false)
        #expect(turn.question?.questionId == "q2")
        #expect(turn.isWaitingOnUser == true)
    }

    @MainActor
    @Test func newUserMessageArchivesPreviousTurn() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.thinking(delta: "reasoning"))
        store.userTurnStarted(anchorMessageID: "m2")

        #expect(store.turns.count == 2)
        #expect(store.liveTurn?.anchorMessageID == "m2")
        #expect(store.turn(anchoredTo: "m1")?.thinkingText == "reasoning")
        #expect(store.turn(anchoredTo: "m2")?.hasContent == false)
    }

    @MainActor
    @Test func duplicateUserTurnForSameMessageIsIdempotent() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.userTurnStarted(anchorMessageID: "m1")
        #expect(store.turns.count == 1)
    }

    @MainActor
    @Test func resetClearsEverything() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.thinking(delta: "x"))
        store.reset()
        #expect(store.turns.isEmpty)
        #expect(store.isAwaitingFirstReply == false)
    }

    @MainActor
    @Test func eventsWithNoTurnAreDropped() {
        let store = ChatActivityStore()
        store.apply(.toolCall(ChatToolCallEvent(id: "t1", tool: "x", arguments: "{}")))
        #expect(store.turns.isEmpty)
    }

    /// 3.3 — a chip + expandable details are constructible from store data.
    @MainActor
    @Test func liveToolChipRendersFromStoreData() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.toolCall(ChatToolCallEvent(id: "t1", tool: "web_search", arguments: "{\"q\":\"x\"}")))
        guard let tool = store.liveTurn?.tools.first else {
            Issue.record("expected a tool call")
            return
        }
        _ = LiveToolChip(tool: tool)
        _ = ToolCallRow(tool: tool)
        #expect(tool.name == "web_search")
        #expect(tool.arguments == #"{"q":"x"}"#)
    }
}

// MARK: - 4.1 / 4.2 — Interactive cards

struct InteractiveCardTests {
    @MainActor
    @Test func approvalCardShowsControlsThenAnsweredOutcome() {
        let request = ApprovalRequest(
            questionId: "q1",
            tool: "delete_file",
            arguments: #"{"path":"/tmp/a"}"#,
            decision: nil
        )
        _ = ApprovalCardView(request: request) { _ in }
        #expect(request.isAnswered == false)

        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.approval(ChatApprovalEvent(questionId: "q1", tool: "delete_file", arguments: "{}")))
        store.submitApproval(questionId: "q1", decision: .reject(reason: "wrong target"))

        guard let turn = store.liveTurn, let approval = turn.approval else {
            Issue.record("expected an answered approval")
            return
        }
        #expect(approval.isAnswered == true)
        #expect(approval.decision == .reject(reason: "wrong target"))
        #expect(turn.isWaitingOnUser == false)
        _ = ApprovalCardView(request: approval) { _ in }
    }

    @MainActor
    @Test func questionCardControlsAndAnsweredState() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.question(ChatQuestionEvent(
            questionId: "q2",
            question: "Which one?",
            interactionType: .buttons,
            options: [ChatQuestionOption(label: "A", value: "a", description: nil)],
            placeholder: nil,
            maxLength: nil
        )))
        guard let question = store.liveTurn?.question else {
            Issue.record("expected a question")
            return
        }
        _ = QuestionCardView(request: question) { _ in }
        #expect(question.isAnswered == false)

        store.submitQuestion(questionId: "q2", answer: "a")
        guard let answered = store.liveTurn?.question else {
            Issue.record("expected an answered question")
            return
        }
        #expect(answered.isAnswered == true)
        #expect(answered.submittedAnswer == "a")
        _ = QuestionCardView(request: answered) { _ in }
        #expect(store.liveTurn?.isWaitingOnUser == false)
    }

    @MainActor
    @Test func decisionsOnTheWrongCardAreIgnored() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.approval(ChatApprovalEvent(questionId: "q1", tool: "x", arguments: "{}")))
        store.submitApproval(questionId: "nope", decision: .approve)
        #expect(store.liveTurn?.approval?.isAnswered == false)
    }
}

// MARK: - 5.1 / 5.2 / 5.3 — Typing indicator, stop, suggested prompts

struct ComposerStateTests {
    @MainActor
    @Test func typingIndicatorShowsUntilFirstReplyToken() {
        let store = ChatActivityStore()
        #expect(store.isAwaitingFirstReply == false)

        store.userTurnStarted(anchorMessageID: "m1")
        #expect(store.isAwaitingFirstReply == true)
        #expect(store.isGeneratingReply == true)
        _ = TypingIndicatorView()

        store.agentReplyStarted()
        #expect(store.isAwaitingFirstReply == false)
        // Still generating (quiet timer not yet fired) — stop stays available.
        #expect(store.isGeneratingReply == true)

        store.markReplyFinished()
        #expect(store.isGeneratingReply == false)
    }

    @MainActor
    @Test func typingIndicatorHidesWhileWaitingOnCard() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.approval(ChatApprovalEvent(questionId: "q1", tool: "x", arguments: "{}")))
        #expect(store.isAwaitingFirstReply == false)
        #expect(store.isGeneratingReply == false)
    }

    @MainActor
    @Test func stopGeneratingReturnsComposerToReady() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        #expect(store.isAwaitingFirstReply == true)
        #expect(store.isGeneratingReply == true)

        store.stopGenerating()
        #expect(store.isAwaitingFirstReply == false)
        #expect(store.isGeneratingReply == false)
    }

    @MainActor
    @Test func suggestedPromptsFillComposerWithoutSending() {
        let prompts = ChatView.suggestedPrompts
        #expect(prompts.count == 4)
        #expect(Set(prompts).count == prompts.count)
        #expect(prompts.allSatisfy { !$0.isEmpty })

        // Each chip is wired to onPick (fills the composer); nothing auto-sends.
        var picked: [String] = []
        _ = SuggestedPromptsView(prompts: prompts) { picked.append($0) }
        #expect(picked.isEmpty)
    }
}
