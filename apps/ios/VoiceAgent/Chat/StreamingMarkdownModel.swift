import Foundation

// MARK: - Streaming model

/// Incremental parse model for a streaming agent reply (issue #441).
///
/// A reply arrives token by token, and `MarkdownRenderer` used to re-parse the
/// whole text on every delta, so a long answer cost O(reply) of markdown
/// parsing (plus a full view rebuild) dozens of times a second. This model
/// splits the text at *block boundaries* and only re-parses the suffix that is
/// still changing:
///
/// - `committed` — blocks parsed from `committedSource`, the text up to the last
///   block boundary. A block's index in `committed` is its id and never changes
///   once committed, so SwiftUI can diff the finished part of the answer.
/// - `tail` — everything after the last boundary. It is re-parsed on every
///   delta, so the per-delta cost is O(tail) instead of O(reply), and it is
///   rendered with the unfinished-syntax fixes in ``MarkdownTailDisplay``.
///
/// A boundary is a blank line *outside* a fenced code block whose following
/// line can start a fresh block by itself: a list marker, block quote marker or
/// indented line may continue the block above it, so those blank lines are not
/// boundaries and the text stays in the tail.
///
/// Streaming is only an optimisation for the intermediate frames. ``finish()``
/// parses the message as one document, so a settled message always equals
/// `parseMarkdown(fullText)`.
struct StreamingMarkdownModel: Equatable {
    /// Text up to (and including) the last committed block boundary.
    private(set) var committedSource = ""
    /// Blocks parsed from `committedSource`; the index is the stable block id.
    private(set) var committed: [MarkdownBlock] = []
    /// Everything after the last committed boundary — the only moving part.
    private(set) var tail = ""
    /// `tail` parsed for display, with the unfinished-syntax fixes applied.
    private(set) var tailBlocks: [MarkdownBlock] = []
    /// The text last handed to ``update(_:)``, so a repeat is a no-op.
    private(set) var appliedText = ""
    /// `true` once the whole message has been parsed as one document.
    private(set) var isSettled = false

    /// All blocks in document order.
    var blocks: [MarkdownBlock] {
        committed + tailBlocks
    }

    /// Applies the latest full text of the message, keeping every block that
    /// ends before the last block boundary.
    mutating func update(_ text: String) {
        guard text != appliedText else { return }
        appliedText = text
        isSettled = false
        if !text.hasPrefix(committedSource) {
            committed = []
            committedSource = ""
        }
        var rest = String(text.dropFirst(committedSource.count))
        if let cut = MarkdownBlockBoundary.committedPrefix(of: rest) {
            rest = commitPrefix(of: rest, upTo: cut)
        }
        tail = rest
        tailBlocks = parseMarkdown(MarkdownTailDisplay.renderable(rest))
    }

    /// Parses the whole message as one document. Called once the stream has
    /// gone quiet, so the last block is styled exactly like history.
    mutating func finish() {
        guard !isSettled else { return }
        committed = parseMarkdown(appliedText)
        committedSource = appliedText
        tail = ""
        tailBlocks = []
        isSettled = true
    }

    /// Promotes `rest[..<cut]` into `committed` and returns the new tail.
    ///
    /// A link reference definition resolves anywhere in a document, so pinning
    /// part of it would change how earlier inlines parse; a message carrying one
    /// (rare in agent output) falls back to re-parsing the whole text per delta.
    private mutating func commitPrefix(of rest: String, upTo cut: String.Index) -> String {
        let chunk = String(rest[..<cut])
        guard !MarkdownBlockBoundary.hasLinkDefinition(chunk) else {
            committed = []
            committedSource = ""
            return appliedText
        }
        committed += parseMarkdown(chunk)
        committedSource += chunk
        return String(rest[cut...])
    }
}

// MARK: - Boundaries

/// Finds the last block boundary a streaming reply has safely reached.
enum MarkdownBlockBoundary {
    /// Offset of the start of the first line after the last usable boundary, or
    /// `nil` when nothing can be committed yet.
    static func committedPrefix(of text: String) -> String.Index? {
        let lines = text.split(separator: "\n", omittingEmptySubsequences: false)
        let starts = lineStarts(of: text, lines: lines)
        var fences = MarkdownFenceTracker()
        var boundary: String.Index?
        for (index, line) in lines.enumerated() {
            let next = index + 1
            if !fences.isInside, isBlank(line), next < lines.count,
               !isBlank(lines[next]), isFreshBlockStart(lines[next])
            {
                boundary = starts[next]
            }
            fences.consume(line)
        }
        return boundary
    }

    /// Whether the chunk splits off a link reference definition.
    static func hasLinkDefinition(_ text: String) -> Bool {
        var fences = MarkdownFenceTracker()
        for line in text.split(separator: "\n", omittingEmptySubsequences: false) {
            if !fences.isInside, isDefinition(line) {
                return true
            }
            fences.consume(line)
        }
        return false
    }

    private static func isDefinition(_ line: Substring) -> Bool {
        let indent = line.prefix { $0 == " " }.count
        guard indent <= 3 else { return false }
        var rest = line.dropFirst(indent)
        guard rest.first == "[" else { return false }
        rest = rest.dropFirst()
        guard let close = rest.firstIndex(of: "]"), close > rest.startIndex else { return false }
        return rest[rest.index(after: close)...].first == ":"
    }

    /// The start offset of every line, in index order.
    private static func lineStarts(of text: String, lines: [Substring]) -> [String.Index] {
        var starts: [String.Index] = []
        starts.reserveCapacity(lines.count)
        var offset = text.startIndex
        for line in lines {
            starts.append(offset)
            offset = text.index(offset, offsetBy: line.count + 1, limitedBy: text.endIndex)
                ?? text.endIndex
        }
        return starts
    }

    private static func isBlank(_ line: Substring) -> Bool {
        line.allSatisfy { $0 == " " || $0 == "\t" }
    }

    /// Any leading space: a line that starts indented can continue the list
    /// item or indented code block above the blank line, so it is never treated
    /// as the start of a fresh block here.
    private static func isIndented(_ line: Substring) -> Bool {
        line.first == " " || line.first == "\t"
    }

    /// Whether `line` can only begin a new block, i.e. it cannot be swallowed
    /// as a continuation of the block before it.
    private static func isFreshBlockStart(_ line: Substring) -> Bool {
        guard !isIndented(line) else { return false }
        guard let first = line.first else { return false }
        return !continuesPreviousBlock(line, first: first)
    }

    private static func continuesPreviousBlock(_ line: Substring, first: Character) -> Bool {
        switch first {
        case ">":
            return true
        case "-", "*", "+":
            return isMarkerOnly(line.dropFirst())
        default:
            break
        }
        guard first.isNumber else { return false }
        let digits = line.prefix { $0.isNumber }
        let afterDigits = line.dropFirst(digits.count)
        guard let marker = afterDigits.first, marker == "." || marker == ")" else { return false }
        return isMarkerOnly(afterDigits.dropFirst())
    }

    private static func isMarkerOnly(_ rest: Substring) -> Bool {
        rest.isEmpty || rest.first == " " || rest.first == "\t"
    }
}
