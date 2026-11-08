import Foundation
@testable import Memory
import Testing

/// Every copy affordance writes a `CopyIntent`. These assert the exact text
/// each surface copies, and that the injected board receives it unchanged.
struct CopyIntentTests {
    @Test func anAssistantMessageCopiesAsPlainReadingText() {
        let markdown = "**Bold** and `code` and ![A duck](https://example.com/duck.png)."
        #expect(CopyIntent.assistantMessage(markdown).text == "Bold and code and A duck.")
    }

    @Test func aUserMessageIsTrimmed() {
        #expect(CopyIntent.userMessage("  hello\n").text == "hello")
    }

    @Test func aCodeBlockCopiesItsSourceOnly() {
        let code = "let answer = 42\n"
        let intent = CopyIntent.code(code)
        #expect(intent.text == code)
        #expect(!intent.text.contains("```"))
        #expect(!intent.text.contains("swift"))
    }

    @Test func aTurnCopiesAsReadingText() {
        #expect(CopyIntent.turn("## Title\n\n**done**").text == "Title\n\ndone")
    }

    @Test func anObjectCardCopiesItsCanonicalID() {
        #expect(CopyIntent.objectID("object-42").text == "object-42")
    }

    @Test func bothRolesCopyTheIntendedText() {
        let assistant = ChatMessage(id: "a", role: .assistant, text: "**Hi**")
        #expect(assistant.copyIntent == CopyIntent(text: "Hi"))

        let user = ChatMessage(id: "u", role: .user, text: "  hi  ")
        #expect(user.copyIntent == CopyIntent(text: "hi"))
    }

    @Test func theInjectedBoardReceivesTheExactText() {
        var written: [String] = []
        let board = CopyBoard(write: { written.append($0) })

        board.write(CopyIntent.code("print(1)").text)
        board.write(ChatMessage(id: "a", role: .assistant, text: "**Hi**").copyIntent.text)

        #expect(written == ["print(1)", "Hi"])
    }
}
