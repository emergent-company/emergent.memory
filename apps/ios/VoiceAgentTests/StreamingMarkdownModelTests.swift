import Foundation
@testable import Memory
import Testing

// MARK: - Committed / tail splitting (issue #441 item 1)

struct StreamingMarkdownModelTests {
    @Test func freezesBlocksUpToTheLastBoundary() {
        var model = StreamingMarkdownModel()
        model.update("First paragraph.\n\nSecond paragraph is still arriving")
        #expect(model.committed == [.paragraph([.text("First paragraph.")])])
        #expect(model.tail == "Second paragraph is still arriving")
        #expect(model.tailBlocks == [.paragraph([.text("Second paragraph is still arriving")])])
        #expect(model.blocks.count == 2)
    }

    @Test func blankLineInsideAFenceIsNotABoundary() {
        var model = StreamingMarkdownModel()
        model.update("Intro.\n\n```swift\nlet one = 1\n\nlet two = 2\n")
        // Only the intro is frozen: the fence is still open, so the blank line
        // inside it must not end the code block.
        #expect(model.committed == [.paragraph([.text("Intro.")])])
        #expect(model.tail.hasPrefix("```swift"))
        guard case let .codeBlock(language, code)? = model.tailBlocks.first else {
            Issue.record("expected the open fence to parse as one code block")
            return
        }
        #expect(language == "swift")
        #expect(code == "let one = 1\n\nlet two = 2")
    }

    @Test func closingFenceCommitsTheWholeCodeBlock() {
        var model = StreamingMarkdownModel()
        model.update("Intro.\n\n```swift\nlet one = 1\n\nlet two = 2\n```\n\nDone.\n\n")
        #expect(model.committed.count == 2)
        guard case let .codeBlock(language, code)? = model.committed.last else {
            Issue.record("expected the closed fence to be committed")
            return
        }
        #expect(language == "swift")
        #expect(code == "let one = 1\n\nlet two = 2")
        #expect(model.tail == "Done.\n\n")
    }

    @Test func blankLineBetweenListItemsIsNotABoundary() {
        var model = StreamingMarkdownModel()
        model.update("- loose one\n\n- loose two\n\n")
        #expect(model.committed.isEmpty, "a blank line between list items continues the list")
        guard case let .list(_, _, items)? = model.tailBlocks.first else {
            Issue.record("expected a single loose list in the tail")
            return
        }
        #expect(items.count == 2)
    }

    @Test func listCommitsOnceAFreshBlockFollows() {
        var model = StreamingMarkdownModel()
        model.update("- loose one\n\n- loose two\n\nParagraph after.\n\n")
        #expect(model.committed == [
            .list(isOrdered: false, startIndex: 1, items: [
                .plain([.paragraph([.text("loose one")])]),
                .plain([.paragraph([.text("loose two")])]),
            ]),
        ])
        #expect(model.tail == "Paragraph after.\n\n")
    }

    @Test func blankLineBetweenQuoteParagraphsIsNotABoundary() {
        var model = StreamingMarkdownModel()
        model.update("> quote one\n\n> quote two\n\n")
        #expect(model.committed.isEmpty)
        guard case let .blockQuote(blocks)? = model.tailBlocks.first else {
            Issue.record("expected a single block quote in the tail")
            return
        }
        #expect(blocks.count == 2)
    }
}

// MARK: - Unfinished syntax in the tail (issue #441 item 2)

struct MarkdownTailDisplayTests {
    @Test func autoCloseUsesTheOpeningFenceMarkerAndLength() {
        // Appending a hard-coded "```" would leave these fences open.
        #expect(MarkdownTailDisplay.renderable("````\ncode\n").hasSuffix("\n````"))
        #expect(MarkdownTailDisplay.renderable("~~~\ncode\n").hasSuffix("\n~~~"))
        let closed = MarkdownTailDisplay.renderable("```swift\nlet answer = 1")
        #expect(closed == "```swift\nlet answer = 1\n```")
        #expect(parseMarkdown(closed) == [
            .codeBlock(language: "swift", code: "let answer = 1"),
        ])
    }

    @Test func closedFenceIsLeftAlone() {
        let text = "```swift\nlet answer = 1\n```\n"
        #expect(MarkdownTailDisplay.renderable(text) == text)
    }

    @Test func danglingDelimitersAreHidden() {
        #expect(MarkdownTailDisplay.removingDanglingDelimiters("This is **bol") == "This is bol")
        #expect(MarkdownTailDisplay.removingDanglingDelimiters("See [the docs") == "See the docs")
        #expect(MarkdownTailDisplay.removingDanglingDelimiters("Run `make") == "Run make")
        #expect(MarkdownTailDisplay.removingDanglingDelimiters("An _italic") == "An italic")
    }

    @Test func completeSpansAndPlainTextAreUntouched() {
        let finished = "**bold** and *italic* and `code`"
        #expect(MarkdownTailDisplay.removingDanglingDelimiters(finished) == finished)
        let plain = "call snake_case_name and multiply 2 * 3"
        #expect(MarkdownTailDisplay.removingDanglingDelimiters(plain) == plain)
    }
}

// MARK: - Equivalence with the whole-document parser

struct StreamingMarkdownEquivalenceTests {
    /// Every construct whose blocks can span a blank line: a fence with a blank
    /// line inside, a loose list, a multi-paragraph quote and a table.
    private let document = """
    # Streaming reply

    A paragraph with **bold**, *italic*, `code` and a [link](https://example.com).

    ```swift
    struct Model {
        let committed: [Block]

        let tail: String
    }
    ```

    - loose one

    - loose two

    | Name | Value |
    |------|-------|
    | A    | 1     |

    > Quote one.

    > Quote two.

    1. first
    2. second

    Final paragraph that ends the reply.
    """

    @Test func finishedDocumentMatchesWholeParse() {
        var model = StreamingMarkdownModel()
        model.update(document)
        model.finish()
        #expect(model.blocks == parseMarkdown(document))
    }

    @Test func characterByCharacterFeedMatchesWholeParse() {
        var model = StreamingMarkdownModel()
        var committed: [MarkdownBlock] = []
        for end in document.indices {
            model.update(String(document[...end]))
            // Freezing is only useful if a committed block never changes.
            #expect(model.committed.starts(with: committed))
            committed = model.committed
        }
        #expect(model.blocks == parseMarkdown(document))
        #expect(!model.committed.isEmpty)
    }

    @Test func lineByLineFeedMatchesWholeParse() {
        var model = StreamingMarkdownModel()
        var text = ""
        for line in document.split(separator: "\n", omittingEmptySubsequences: false) {
            text += line + "\n"
            model.update(text)
        }
        #expect(model.blocks == parseMarkdown(document))
    }
}
