import Foundation
import Splash

/// Fenced-code syntax highlighting via Splash. Kept in its own file (and
/// SwiftUI-free) because Splash declares top-level types (`Font`, `Color`,
/// `Theme`) that would shadow SwiftUI's in a view file.
enum MarkdownSyntaxHighlighter {
    /// Highlights fenced code with Splash. Returns `nil` (caller falls back
    /// to plain monospaced text) on empty input or any highlight failure.
    ///
    /// Splash 0.16 ships only the Swift grammar, so it is used for every
    /// language; unrecognized languages simply tokenize conservatively
    /// instead of failing.
    static func highlight(_ code: String, language: String?) -> NSAttributedString? {
        let trimmed = code.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        let theme = Splash.Theme.midnight(withFont: Splash.Font(size: 13))
        let highlighter = Splash.SyntaxHighlighter(
            format: Splash.AttributedStringOutputFormat(theme: theme),
            grammar: SwiftGrammar()
        )
        return highlighter.highlight(code)
    }
}
