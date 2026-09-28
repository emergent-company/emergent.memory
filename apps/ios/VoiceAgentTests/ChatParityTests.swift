import Foundation
@testable import Memory
import SwiftUI
import Testing

// MARK: - 1.2 / 1.3 / 1.4 / 1.5 — Markdown parsing

struct MarkdownParserTests {
    @Test func parsesHeadingsParagraphsAndLists() throws {
        let md = """
        # Title

        A paragraph with *emphasis*, **strong**, `code`, and a [link](https://example.com).

        ## Sub

        - one
        - two
        """
        let blocks = parseMarkdown(md)
        try #require(blocks.count == 4)
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

    @Test func parsesFencedCodeBlock() throws {
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
        try #require(codeBlocks.count == 1)
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

struct MarkdownSyntaxHighlighterTests {
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

struct ChatEventDecodingTests {
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

// MARK: - 4.3 — Decision encoding

struct ChatDecisionEncodingTests {
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

// MARK: - 1.6 — Shared tool-call components

struct ToolCallComponentTests {
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
