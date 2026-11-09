import Foundation
@testable import Memory
import Testing

/// Copy message/turn puts the reply's reading text on the pasteboard, not its
/// markdown source; a code block's copy control takes the block's source only.
struct MarkdownPlainTextTests {
    @Test func aCodeBlocksCopyTextIsItsSourceOnly() {
        guard case let .codeBlock(language, code)? = parseMarkdown("```swift\nlet answer = 42\n```\n").first else {
            Issue.record("expected the fenced block to parse as one code block")
            return
        }

        // The copy control in MarkdownBlockView passes exactly this `code`: no
        // fence markers, no language label.
        #expect(language == "swift")
        #expect(code == "let answer = 42\n")
        #expect(!code.contains("```"))
    }

    @Test func copyTextDropsInlineMarkersButKeepsTheProse() {
        let text = "**Bold** and `code` and [a link](https://example.com).\n\nSecond paragraph."
        #expect(markdownPlainText(text) == "Bold and code and a link.\n\nSecond paragraph.")
    }

    @Test func headingsAndCodeBlocksKeepTheirTextWithoutMarkers() {
        let text = "## Heading\n\n```swift\nlet answer = 42\n```"
        #expect(markdownPlainText(text) == "Heading\n\nlet answer = 42")
    }

    @Test func listsReadAsItemsWithoutMarkers() {
        #expect(markdownPlainText("- one\n- two\n") == "one\ntwo")
    }

    @Test func blockQuotesReadAsTheirText() {
        #expect(markdownPlainText("> quoted\n") == "quoted")
    }

    @Test func inlineImagesReadAsTheirAltText() {
        #expect(
            markdownPlainText("Before ![A duck](https://example.com/duck.png) after.")
                == "Before A duck after."
        )
    }

    @Test func aLinkedImageKeepsItsAltTextAndURL() {
        #expect(
            markdownPlainText("[![A duck](https://example.com/duck.png)](https://example.com/duck)")
                == "A duck (https://example.com/duck)"
        )
    }

    @Test func aBlockImageReadsAsItsAltText() {
        #expect(markdownPlainText("![A duck](https://example.com/duck.png)") == "A duck")
    }

    @Test func tablesCopyAsTabSeparatedRowsWithoutMarkers() {
        let text = """
        | Name | Age |
        | --- | --- |
        | Ada | 36 |
        | Bob | 41 |
        """
        let plain = markdownPlainText(text)
        #expect(plain == "Name\tAge\nAda\t36\nBob\t41")
        #expect(!plain.contains("|"))
        #expect(!plain.contains("---"))
    }

    @Test func nestedListsKeepEveryItemWithoutMarkers() {
        let text = """
        - parent
          - child
        - second
        """
        let plain = markdownPlainText(text)
        #expect(plain.contains("parent"))
        #expect(plain.contains("child"))
        #expect(plain.contains("second"))
        #expect(!plain.contains("- "))
    }

    @Test func rawHTMLBlocksStayAsTextInsteadOfDisappearing() {
        let plain = markdownPlainText("<div class=\"note\">hello</div>")
        #expect(plain.contains("<div"))
        #expect(plain.contains("hello"))
    }
}
