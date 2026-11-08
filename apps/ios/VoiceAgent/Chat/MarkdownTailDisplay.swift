import Foundation

// MARK: - Fences

/// Tracks whether a document is inside a fenced code block.
struct MarkdownFenceTracker {
    private(set) var isInside = false
    private(set) var marker: Character = "`"
    private(set) var length = 0

    /// The fence still open at the end of `text`, if any.
    static func openFence(in text: String) -> (marker: Character, length: Int)? {
        var tracker = MarkdownFenceTracker()
        for line in text.split(separator: "\n", omittingEmptySubsequences: false) {
            tracker.consume(line)
        }
        guard tracker.isInside else { return nil }
        return (tracker.marker, tracker.length)
    }

    /// Feeds one line, opening or closing the fence when the line is one.
    /// Returns `true` when the line was a fence, so callers can tell code
    /// syntax apart from prose.
    @discardableResult
    mutating func consume(_ line: Substring) -> Bool {
        var text = line
        var indent = 0
        while indent < 3, text.first == " " {
            text = text.dropFirst()
            indent += 1
        }
        guard let first = text.first, first == "`" || first == "~" else { return false }
        let run = text.prefix { $0 == first }
        if isInside {
            guard first == marker, run.count >= length else { return false }
            let trailer = text.dropFirst(run.count)
            guard trailer.allSatisfy({ $0 == " " || $0 == "\t" }) else { return false }
            isInside = false
        } else {
            guard run.count >= 3 else { return false }
            // A backtick fence's info string may not contain a backtick.
            if first == "`", text.dropFirst(run.count).contains("`") { return false }
            isInside = true
            marker = first
            length = run.count
        }
        return true
    }
}

// MARK: - Tail display

/// Rendering-only fixes for the tail while it is still arriving (issue #441
/// item 2). Nothing here is written back to the message text: the tail borrows
/// a closing fence or hides a half-typed delimiter so the last block does not
/// flicker between styles as tokens land.
enum MarkdownTailDisplay {
    /// The tail as it should be parsed for display.
    static func renderable(_ tail: String) -> String {
        guard !tail.isEmpty else { return "" }
        if let open = MarkdownFenceTracker.openFence(in: tail) {
            return withClosingFence(tail, open: open)
        }
        return removingDanglingDelimiters(tail)
    }

    /// An odd number of fences leaves the last code block open. Close it (with
    /// the opener's own marker and length) so the block renders as one finished,
    /// unhighlighted code block instead of growing into the following text.
    static func withClosingFence(
        _ text: String,
        open: (marker: Character, length: Int)
    ) -> String {
        var closed = text
        if !closed.hasSuffix("\n") {
            closed.append("\n")
        }
        closed.append(String(repeating: open.marker, count: open.length))
        return closed
    }

    /// Hides a delimiter run that has no closing counterpart yet, so a
    /// half-typed `**bold`, `` `code `` or `[link` renders as plain text
    /// instead of painting punctuation that vanishes on the next token.
    static func removingDanglingDelimiters(_ text: String) -> String {
        let segmentStart = lastBlockStart(in: text)
        let segment = text[segmentStart...]
        var removals: [Range<String.Index>] = []
        for marker: Character in ["*", "`", "_"] {
            if let range = unmatchedDelimiter(marker, in: segment) {
                removals.append(range)
            }
        }
        if let bracket = unmatchedBracket(in: segment) {
            removals.append(bracket)
        }
        guard !removals.isEmpty else { return text }
        var result = String(text[..<segmentStart])
        var cursor = segment.startIndex
        for range in removals.sorted(by: { $0.lowerBound < $1.lowerBound }) {
            guard range.lowerBound >= cursor else { continue }
            result += text[cursor..<range.lowerBound]
            cursor = range.upperBound
        }
        result += text[cursor...]
        return result
    }

    /// Where the part of the tail that can carry a half-typed delimiter begins:
    /// after the last blank line and after the last fence line. Delimiters on a
    /// fence line are code-block syntax, not an open span (`closedFenceIsLeftAlone`).
    private static func lastBlockStart(in text: String) -> String.Index {
        var start = text.startIndex
        var cursor = text.startIndex
        var fences = MarkdownFenceTracker()
        while cursor < text.endIndex {
            let lineEnd = text[cursor...].firstIndex(of: "\n") ?? text.endIndex
            let line = text[cursor..<lineEnd]
            let next = lineEnd == text.endIndex ? text.endIndex : text.index(after: lineEnd)
            if fences.consume(line) {
                start = next
            } else if line.allSatisfy({ $0 == " " || $0 == "\t" }), lineEnd < text.endIndex {
                start = next
            }
            cursor = next
        }
        return start
    }

    /// The last delimiter run that opens a span nothing closes.
    private static func unmatchedDelimiter(
        _ marker: Character,
        in segment: Substring
    ) -> Range<String.Index>? {
        var openers: [Range<String.Index>] = []
        var index = segment.startIndex
        while index < segment.endIndex {
            guard segment[index] == marker else {
                index = segment.index(after: index)
                continue
            }
            let run = segment[index...].firstIndex { $0 != marker } ?? segment.endIndex
            let range = index..<run
            if closes(marker, range, in: segment), !openers.isEmpty {
                openers.removeLast()
            } else if opens(marker, range, in: segment) {
                openers.append(range)
            }
            index = run
        }
        return openers.last
    }

    /// Whether the run can open a span: it must be followed by content, and an
    /// underscore must not sit inside a word (`snake_case` is not emphasis).
    private static func opens(
        _ marker: Character,
        _ run: Range<String.Index>,
        in segment: Substring
    ) -> Bool {
        guard let after = characterAfter(run, in: segment), !after.isWhitespace else {
            return false
        }
        guard marker == "_", let before = characterBefore(run, in: segment) else {
            return true
        }
        return before.isWhitespace || before.isPunctuation
    }

    /// Whether the run can close a span: it must follow content, and an
    /// underscore must not sit inside a word.
    private static func closes(
        _ marker: Character,
        _ run: Range<String.Index>,
        in segment: Substring
    ) -> Bool {
        guard let before = characterBefore(run, in: segment), !before.isWhitespace else {
            return false
        }
        guard marker == "_", let after = characterAfter(run, in: segment) else {
            return true
        }
        return after.isWhitespace || after.isPunctuation
    }

    private static func characterBefore(
        _ run: Range<String.Index>,
        in segment: Substring
    ) -> Character? {
        guard run.lowerBound > segment.startIndex else { return nil }
        return segment[segment.index(before: run.lowerBound)]
    }

    private static func characterAfter(
        _ run: Range<String.Index>,
        in segment: Substring
    ) -> Character? {
        guard run.upperBound < segment.endIndex else { return nil }
        return segment[run.upperBound]
    }

    /// An unmatched `[` (a half-typed link label).
    private static func unmatchedBracket(in segment: Substring) -> Range<String.Index>? {
        var open: String.Index?
        for index in segment.indices {
            if segment[index] == "[" {
                open = index
            } else if segment[index] == "]" {
                open = nil
            }
        }
        guard let start = open else { return nil }
        return start..<segment.index(after: start)
    }
}
