import Foundation

/// A reply's markdown as plain reading text — what "Copy message" and "Copy
/// turn" put on the pasteboard: the prose the reader sees, not the source with
/// its styling markers.
///
/// The web copies the rendered node's `innerText` (`enhanceMessage`,
/// `chat-components.js`). This mirrors that: inline emphasis and link syntax
/// drop away, a fenced block keeps its source (the source *is* the text) but
/// loses its fence and language label, and blocks read as paragraphs.
func markdownPlainText(_ text: String) -> String {
    joinedPlainText(of: parseMarkdown(text))
        .trimmingCharacters(in: .whitespacesAndNewlines)
}

/// Joins a block list into reading text: a blank line between blocks, one line
/// inside a list or table.
func joinedPlainText(of blocks: [MarkdownBlock]) -> String {
    blocks
        .map { $0.plainText.trimmingCharacters(in: .whitespacesAndNewlines) }
        .filter { !$0.isEmpty }
        .joined(separator: "\n\n")
}

extension MarkdownBlock {
    /// This block as plain reading text, without markdown markers.
    var plainText: String {
        switch self {
        case let .heading(_, inlines), let .paragraph(inlines):
            return inlines.map(\.plainText).joined()
        case let .codeBlock(_, code):
            return code
        case let .blockQuote(blocks):
            return joinedPlainText(of: blocks)
        case let .list(_, _, items):
            // The web's innerText omits generated list markers, so the text
            // reads as the items alone, one per line.
            return items
                .map(\.plainText)
                .joined(separator: "\n")
        case let .table(table):
            let header = table.headers.map { $0.map(\.plainText).joined() }
            let rows = table.rows.map { row in
                row.map { $0.map(\.plainText).joined() }
            }
            return ([header] + rows)
                .map { $0.joined(separator: "\t") }
                .joined(separator: "\n")
        case let .image(_, alt):
            return alt ?? ""
        case .thematicBreak:
            return ""
        }
    }
}

extension MarkdownListItem {
    /// This item's blocks as reading text.
    var plainText: String {
        switch self {
        case let .plain(blocks):
            joinedPlainText(of: blocks)
        case let .task(_, blocks):
            joinedPlainText(of: blocks)
        }
    }
}
