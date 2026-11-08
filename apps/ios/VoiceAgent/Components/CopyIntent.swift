import SwiftUI

/// What one copy affordance writes. One type for every surface — a reply's
/// action row, its context menu, a fenced code block, a turn footer, an object
/// card — so a test can assert the exact text each one copies and no view can
/// quietly copy something else.
struct CopyIntent: Equatable {
    let text: String

    /// An assistant reply's markdown, copied as plain reading text.
    static func assistantMessage(_ markdown: String) -> CopyIntent {
        CopyIntent(text: markdownPlainText(markdown.trimmingCharacters(in: .whitespacesAndNewlines)))
    }

    /// A user's own message; already plain.
    static func userMessage(_ text: String) -> CopyIntent {
        CopyIntent(text: text.trimmingCharacters(in: .whitespacesAndNewlines))
    }

    /// A fenced code block's source only: no fence, no language label.
    static func code(_ code: String) -> CopyIntent {
        CopyIntent(text: code)
    }

    /// A whole turn's reading text (same rule as an assistant reply).
    static func turn(_ text: String) -> CopyIntent {
        CopyIntent(text: markdownPlainText(text.trimmingCharacters(in: .whitespacesAndNewlines)))
    }

    /// An object card's canonical id.
    static func objectID(_ id: String) -> CopyIntent {
        CopyIntent(text: id)
    }
}

extension ChatMessage {
    /// The text both the action row and the context menu write.
    var copyIntent: CopyIntent {
        role == .assistant ? .assistantMessage(text) : .userMessage(text)
    }
}

/// The one injected sink every copy affordance writes through. Tests replace it
/// to assert a copy without the simulator's shared pasteboard.
struct CopyBoard {
    let write: (String) -> Void

    /// The shipped pasteboard write.
    static var system: CopyBoard {
        CopyBoard(write: CopyButton.writeToPasteboard)
    }
}

extension EnvironmentValues {
    @Entry var copyBoard: CopyBoard = .system
}
