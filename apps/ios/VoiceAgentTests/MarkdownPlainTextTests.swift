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
}
