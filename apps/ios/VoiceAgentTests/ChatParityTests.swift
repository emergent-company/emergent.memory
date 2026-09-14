import Foundation
import SwiftUI
import Testing
@testable import Memory

// MARK: - 1.2 / 1.3 / 1.4 / 1.5 — Markdown parsing

@Suite struct MarkdownParserTests {
    @Test func parsesHeadingsParagraphsAndLists() {
        let md = """
        # Title

        A paragraph with *emphasis*, **strong**, `code`, and a [link](https://example.com).

        ## Sub

        - one
        - two
        """
        let blocks = parseMarkdown(md)
        #expect(blocks.count == 4)
        #expect(blocks[0] == .heading(level: 1, inlines: [.text("Title")]))
        #expect(blocks[2] == .heading(level: 2, inlines: [.text("Sub")]))

        guard case let .paragraph(inlines) = blocks[1] else {
            Issue.record("expected paragraph")
            return
        }
        #expect(inlines.contains(.emphasis([.text("emphasis")])))
        #expect(inlines.contains(.strong([.text("strong")])))
        #expect(inlines.contains(.code("code")))
        #expect(inlines.contains(.link(destination: "https://example.com", inlines: [.text("link")])))

        guard case let .list(isOrdered, startIndex, items) = blocks[3] else {
            Issue.record("expected list")
            return
        }
        #expect(!isOrdered)
        #expect(startIndex == 1)
        #expect(items == [
            .plain([.paragraph([.text("one")])]),
            .plain([.paragraph([.text("two")])]),
        ])
    }

    @Test func parsesFencedCodeBlock() {
        let md = """
        Before

        ```swift
        let x = 1
        print(x)
        ```

        After
        """
        let blocks = parseMarkdown(md)
        let codeBlocks = blocks.compactMap { block -> (String?, String)? in
            guard case let .codeBlock(language, code) = block else { return nil }
            return (language, code)
        }
        #expect(codeBlocks.count == 1)
        #expect(codeBlocks[0].0 == "swift")
        #expect(codeBlocks[0].1.contains("let x = 1"))
    }

    @Test func parsesBlockquote() {
        let md = "> quoted text"
        let blocks = parseMarkdown(md)
        #expect(blocks == [.blockQuote([.paragraph([.text("quoted text")])])])
    }

    @Test func parsesTable() {
        let md = """
        | A | B |
        |---|---|
        | 1 | 2 |
        """
        let blocks = parseMarkdown(md)
        guard case let .table(table)? = blocks.first else {
            Issue.record("expected a table block")
            return
        }
        #expect(table.headers == [[.text("A")], [.text("B")]])
        #expect(table.rows == [[[.text("1")], [.text("2")]]])
    }

    @Test func parsesTaskList() {
        let md = """
        - [x] done item
        - [ ] todo item
        """
        let blocks = parseMarkdown(md)
        guard case let .list(_, _, items)? = blocks.first, items.count == 2 else {
            Issue.record("expected a two-item list")
            return
        }
        #expect(items[0] == .task(isChecked: true, blocks: [.paragraph([.text("done item")])]))
        #expect(items[1] == .task(isChecked: false, blocks: [.paragraph([.text("todo item")])]))
    }

    @Test func parsesStandaloneImage() {
        let md = "![A chart](https://example.com/chart.png)"
        let blocks = parseMarkdown(md)
        #expect(blocks == [.image(source: "https://example.com/chart.png", alt: "A chart")])
    }

    @Test func parsesOrderedListWithStart() {
        let md = "3. three\n4. four"
        let blocks = parseMarkdown(md)
        guard case let .list(isOrdered, startIndex, items)? = blocks.first else {
            Issue.record("expected ordered list")
            return
        }
        #expect(isOrdered)
        #expect(startIndex == 3)
        #expect(items.count == 2)
    }

    @Test func malformedMarkdownDegradesWithoutCrashing() {
        let fixtures = [
            "~~~ unclosed fence\n**bold never closed\n",
            "[link](http://unterminated\n\n- list\n- \n",
            "| broken table\n| no separator\n",
            "~~strike\n\n###\n#\n",
            "\n\n\n",
        ]
        for fixture in fixtures {
            let blocks = parseMarkdown(fixture)
            // Must not crash; may yield zero blocks (renderer falls back to plain text).
            _ = blocks
            _ = MarkdownRenderer(text: fixture)
        }
    }

    @Test func rendersComplexMarkdownWithoutCrashing() {
        let md = """
        # Hello *world*

        Text with **bold**, *italic*, `inline code`, ~~struck~~ and a [link](https://example.com).

        > a quote with **bold**

        | Name | Value |
        |------|-------|
        | A    | 1     |

        - [x] done
        - [ ] todo

        ```python
        def f():
            return 42
        ```

        ![img](https://example.com/i.png)
        """
        _ = MarkdownRenderer(text: md)
        #expect(!parseMarkdown(md).isEmpty)
    }
}

// MARK: - Syntax highlighting (1.3)

@Suite struct MarkdownSyntaxHighlighterTests {
    @Test func highlightsSwiftCode() {
        let output = MarkdownSyntaxHighlighter.highlight("let x = 1", language: "swift")
        #expect(output != nil)
    }

    @Test func returnsNilForEmptyInput() {
        #expect(MarkdownSyntaxHighlighter.highlight("   \n", language: "swift") == nil)
    }

    @Test func unknownLanguageAndGarbageDoNotCrash() {
        // Splash core ships only the Swift grammar; anything else degrades
        // gracefully instead of failing.
        #expect(MarkdownSyntaxHighlighter.highlight("def f(): pass", language: "python") != nil)
        #expect(MarkdownSyntaxHighlighter.highlight("}}}}))((;;;", language: nil) != nil)
    }
}

// MARK: - 3.1 — Chat event decoding

@Suite struct ChatEventDecodingTests {
    @Test func decodesToolCall() {
        let json = #"{"type":"tool_call","id":"t1","tool":"web_search","arguments":"{\"q\":\"memory\"}"}"#
        guard let event = decodeChatEvent(json) else {
            Issue.record("expected a tool_call event")
            return
        }
        #expect(event == .toolCall(ChatToolCallEvent(id: "t1", tool: "web_search", arguments: #"{"q":"memory"}"#)))
    }

    @Test func decodesToolResult() {
        let json = #"{"type":"tool_result","id":"t1","tool":"web_search","result":"3 hits","isError":false}"#
        guard let event = decodeChatEvent(json) else {
            Issue.record("expected a tool_result event")
            return
        }
        #expect(event == .toolResult(ChatToolResultEvent(id: "t1", tool: "web_search", result: "3 hits", isError: false)))
    }

    @Test func decodesErrorToolResult() {
        let json = #"{"type":"tool_result","id":"t1","tool":"delete_file","result":"no such file","isError":true}"#
        guard let event = decodeChatEvent(json) else {
            Issue.record("expected a tool_result event")
            return
        }
        #expect(event == .toolResult(ChatToolResultEvent(id: "t1", tool: "delete_file", result: "no such file", isError: true)))
    }

    @Test func decodesThinkingDelta() {
        let json = #"{"type":"thinking","delta":"step one"}"#
        guard let event = decodeChatEvent(json) else {
            Issue.record("expected a thinking event")
            return
        }
        #expect(event == .thinking(delta: "step one"))
    }

    @Test func decodesApproval() {
        let json = #"{"type":"approval","questionId":"q1","tool":"delete_file","arguments":"{\"path\":\"/tmp/x\"}"}"#
        guard let event = decodeChatEvent(json) else {
            Issue.record("expected an approval event")
            return
        }
        #expect(event == .approval(ChatApprovalEvent(questionId: "q1", tool: "delete_file", arguments: #"{"path":"/tmp/x"}"#)))
    }

    @Test func decodesButtonsQuestion() {
        let json = #"""
        {"type":"question","questionId":"q2","question":"Which one?","interactionType":"buttons","options":[{"label":"A","value":"a","description":"first"},{"label":"B","value":"b"}],"placeholder":"","maxLength":0}
        """#
        guard case let .question(question)? = decodeChatEvent(json) else {
            Issue.record("expected a question event")
            return
        }
        #expect(question.questionId == "q2")
        #expect(question.interactionType == .buttons)
        #expect(question.options == [
            ChatQuestionOption(label: "A", value: "a", description: "first"),
            ChatQuestionOption(label: "B", value: "b", description: nil),
        ])
    }

    @Test func decodesMultiSelectAndFreeTextInteractionTypes() {
        guard case let .question(multiQuestion)? = decodeChatEvent(#"{"type":"question","questionId":"q1","question":"Pick","interactionType":"multi_select","options":[]}"#) else {
            Issue.record("expected a multi_select question")
            return
        }
        #expect(multiQuestion.interactionType == .multiSelect)

        guard case let .question(freeQuestion)? = decodeChatEvent(#"{"type":"question","questionId":"q1","question":"Type","interactionType":"free_text","options":[],"placeholder":"say hi","maxLength":140}"#) else {
            Issue.record("expected a free_text question")
            return
        }
        #expect(freeQuestion.interactionType == .freeText)
        #expect(freeQuestion.placeholder == "say hi")
        #expect(freeQuestion.maxLength == 140)
    }

    @Test func ignoresUnknownTypesAndGarbage() {
        #expect(decodeChatEvent(#"{"type":"mystery","data":1}"#) == nil)
        #expect(decodeChatEvent("not json") == nil)
        #expect(decodeChatEvent("") == nil)
    }
}

// MARK: - 3.2 / 3.4 — Activity store: correlation + accumulation

@Suite struct ChatActivityStoreTests {
    @MainActor
    @Test func correlatesToolCallsAndResults() {
        let store = ChatActivityStore()
        store.userTurnStarted(anchorMessageID: "m1")
        store.apply(.toolCall(ChatToolCallEvent(id: "t1", tool: "web_search", arguments: "{}")))
        store.apply(.toolCall(ChatToolCallEvent(id: "t2", tool: "read_file", arguments: "{\"p\":\"/x\"}")))

        guard let turn = store.liveTurn else {
            Issue.record("expected a live turn")
            return
        }
        let toolIDs = turn.tools.map { $0.id }
        let allRunning = turn.tools.allSatisfy { $0.isRunning }
        #expect(toolIDs == ["t1", "t2"])
        #expect(allRunning)

        // Result for t2 arrives first; t1 still running.
        store.apply(.toolResult(ChatToolResultEvent(id: "t2", tool: "read_file", result: "file body", isError: false)))
        guard let afterT2 = store.liveTurn else {
            Issue.record("expected a live turn")
            return
        }
        #expect(afterT2.tools[1].result == "file body")
        #expect(afterT2.tools[1].isRunning == false)
        #expect(afterT2.tools[0].isRunning == true)

        store.apply(.toolResult(ChatToolResultEvent(id: "t1", tool: "web_search", result: "boom", isError: true)))
        guard let finalTurn = store.liveTurn else {
            Issue.record("expected a live turn")
            return
        }
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

    // 3.3 — a chip + expandable details are constructible from store data.
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

@Suite struct InteractiveCardTests {
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

// MARK: - 4.3 — Decision encoding

@Suite struct ChatDecisionEncodingTests {
    @Test func encodesApproval() {
        guard let object = decisionObject(for: .approve(questionId: "q1")) else { return }
        #expect(object["type"] == "approval")
        #expect(object["questionId"] == "q1")
        #expect(object["action"] == "approve")
        #expect(object["message"] == nil)
        #expect(object["answer"] == nil)
    }

    @Test func encodesRejectWithoutReason() {
        guard let object = decisionObject(for: .reject(questionId: "q1", reason: nil)) else { return }
        #expect(object["type"] == "approval")
        #expect(object["action"] == "reject")
        #expect(object["message"] == nil)
    }

    @Test func encodesRejectWithReason() {
        guard let object = decisionObject(for: .reject(questionId: "q1", reason: "too risky")) else { return }
        #expect(object["type"] == "approval")
        #expect(object["action"] == "reject")
        #expect(object["message"] == "too risky")
    }

    @Test func encodesQuestionAnswer() {
        guard let object = decisionObject(for: .answer(questionId: "q2", value: "b")) else { return }
        #expect(object["type"] == "question")
        #expect(object["questionId"] == "q2")
        #expect(object["answer"] == "b")
        #expect(object["action"] == nil)
        #expect(object["message"] == nil)
    }

    /// Encodes a decision and flattens it to a string dictionary.
    private func decisionObject(for decision: ChatDecision) -> [String: String]? {
        guard
            let json = try? encodeChatDecision(decision),
            let data = json.data(using: .utf8),
            let object = try? JSONSerialization.jsonObject(with: data) as? [String: String]
        else {
            Issue.record("decision did not encode")
            return nil
        }
        return object
    }
}

// MARK: - 5.1 / 5.2 / 5.3 — Typing indicator, stop, suggested prompts

@Suite struct ComposerStateTests {
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

// MARK: - 1.6 — Shared tool-call components

@Suite struct ToolCallComponentTests {
    @Test func rowAndCardRenderNameArgumentsResultAndError() {
        let ok = ToolCallInfo(id: "t1", name: "web_search", arguments: #"{"q":"x"}"#, result: "3 hits", isError: false)
        let failed = ToolCallInfo(id: "t2", name: "delete_file", arguments: #"{"p":"/x"}"#, result: "permission denied", isError: true)

        _ = ToolCallRow(tool: ok)
        _ = ToolCallRow(tool: failed)
        _ = LiveToolChip(tool: ToolCallInfo(id: "t3", name: "running_tool", arguments: "{}", result: nil, isError: false, isRunning: true))
        _ = ToolCallsCard(tools: [ok, failed], isError: true)

        #expect(ok.isRunning == false)
        #expect(failed.isError == true)
        #expect(ok.result == "3 hits")
    }
}
