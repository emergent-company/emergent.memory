import Foundation
import Splash

/// Fenced-code syntax highlighting via Splash. Kept in its own file (and
/// SwiftUI-free) because Splash declares top-level types (`Font`, `Color`,
/// `Theme`) that would shadow SwiftUI's in a view file.
enum MarkdownSyntaxHighlighter {
    /// Highlights fenced code with Splash. Returns `nil` (caller falls back
    /// to plain monospaced text) on empty input or any highlight failure.
    ///
    /// `isDark` selects a light or dark Splash theme so the token colors stay
    /// legible on the adaptive code surface the renderer draws (Splash ships
    /// only dark-ish defaults otherwise).
    ///
    /// Splash 0.16 ships only the Swift grammar, so it is used for every
    /// language; unrecognized languages simply tokenize conservatively
    /// instead of failing.
    static func highlight(_ code: String, language _: String?, isDark: Bool) -> NSAttributedString? {
        let trimmed = code.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        let font = Splash.Font(size: 13)
        let theme = isDark
            ? Splash.Theme.midnight(withFont: font)
            : Splash.Theme.presentation(withFont: font)
        let highlighter = Splash.SyntaxHighlighter(
            format: Splash.AttributedStringOutputFormat(theme: theme),
            grammar: SwiftGrammar()
        )
        return highlighter.highlight(code)
    }
}
