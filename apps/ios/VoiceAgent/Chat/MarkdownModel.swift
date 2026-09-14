import Foundation
import Markdown

// MARK: - Block model

/// A rich block-level element parsed from agent markdown. Pure value types so
/// parsing is unit-testable without a view.
///
/// IMPORTANT: this file must not `import SwiftUI` — `swift-markdown` declares
/// top-level types (`Text`, `Image`, `Link`, ...) that would shadow SwiftUI's.
/// The SwiftUI renderer lives in `MarkdownRenderer.swift`.
enum MarkdownBlock: Equatable {
    case heading(level: Int, inlines: [MarkdownInline])
    case paragraph([MarkdownInline])
    case codeBlock(language: String?, code: String)
    case blockQuote([MarkdownBlock])
    case list(isOrdered: Bool, startIndex: Int, items: [MarkdownListItem])
    case table(MarkdownTable)
    /// A standalone image (own paragraph): remote http/https rendered with
    /// `AsyncImage`, anything else falls back to the alt text.
    case image(source: String?, alt: String?)
    case thematicBreak
}

enum MarkdownListItem: Equatable {
    case plain([MarkdownBlock])
    case task(isChecked: Bool, blocks: [MarkdownBlock])
}

struct MarkdownTable: Equatable {
    let headers: [[MarkdownInline]]
    let rows: [[[MarkdownInline]]]

    var columnCount: Int { headers.count }
}

/// An inline element inside a paragraph/heading/etc.
enum MarkdownInline: Equatable {
    case text(String)
    /// A newline in the source inside a paragraph (rendered as a space-break).
    case softBreak
    case lineBreak
    case emphasis([MarkdownInline])
    case strong([MarkdownInline])
    case strikethrough([MarkdownInline])
    case code(String)
    case link(destination: String?, inlines: [MarkdownInline])
    case image(source: String?, alt: String?)
    case rawHTML(String)
}

// MARK: - Parsing

/// Parses agent markdown into the typed block model using `swift-markdown`
/// (`Document(parsing:)`). Unknown/unsupported constructs degrade to plain
/// text instead of crashing, so malformed agent output stays readable.
func parseMarkdown(_ text: String) -> [MarkdownBlock] {
    let document = Markdown.Document(parsing: text)
    let lines = text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
    return convertBlocks(document.children, lines: lines)
}

private func convertBlocks(_ children: MarkupChildren, lines: [String]) -> [MarkdownBlock] {
    children.compactMap { convertBlock($0, lines: lines) }
}

private func convertBlock(_ markup: Markup, lines: [String]) -> MarkdownBlock? {
    if let heading = markup as? Markdown.Heading {
        return .heading(level: max(1, heading.level), inlines: convertInlines(heading.children))
    }
    if let paragraph = markup as? Markdown.Paragraph {
        // A paragraph that is only an image becomes a block image.
        if paragraph.childCount == 1, let image = paragraph.child(at: 0) as? Markdown.Image {
            return .image(source: image.source, alt: inlinePlainText(image.children))
        }
        return .paragraph(convertInlines(paragraph.children))
    }
    if let code = markup as? Markdown.CodeBlock {
        let language = code.language?.trimmingCharacters(in: .whitespacesAndNewlines)
        return .codeBlock(language: language?.isEmpty == false ? language : nil, code: code.code)
    }
    if let quote = markup as? Markdown.BlockQuote {
        return .blockQuote(convertBlocks(quote.children, lines: lines))
    }
    if let list = markup as? Markdown.UnorderedList {
        return .list(isOrdered: false, startIndex: 1, items: convertListItems(list.children, lines: lines))
    }
    if let list = markup as? Markdown.OrderedList {
        return .list(isOrdered: true, startIndex: Int(list.startIndex), items: convertListItems(list.children, lines: lines))
    }
    if let table = markup as? Markdown.Table {
        return convertTable(table)
    }
    if markup is Markdown.ThematicBreak {
        return .thematicBreak
    }
    if markup is Markdown.HTMLBlock {
        // Raw HTML blocks are not rendered; degrade to plain text.
        let text = markup.format().trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }
        return .paragraph([.text(text)])
    }
    if markup is Markdown.BlockDirective || markup is Markdown.CustomBlock {
        return nil
    }
    // Unknown block — degrade to its source text so content is never lost.
    let text = markup.format().trimmingCharacters(in: .whitespacesAndNewlines)
    guard !text.isEmpty else { return nil }
    return .paragraph([.text(text)])
}

private func convertListItems(_ children: MarkupChildren, lines: [String]) -> [MarkdownListItem] {
    var items: [MarkdownListItem] = []
    for child in children {
        guard let item = child as? Markdown.ListItem else { continue }
        var blocks = convertBlocks(item.children, lines: lines)
        // Determine checkbox state from the ORIGINAL source line rather than the
        // converted inline text. swift-markdown's GFM tasklist extension is
        // registered lazily: once active it recognizes the marker itself and
        // strips it from the text (exposing it only via the module-internal
        // `ListItem.checkbox`), so text-based detection alone is not reliable.
        guard let isChecked = taskMarkerForItem(item, lines: lines) else {
            items.append(.plain(blocks))
            continue
        }
        // If the marker survived into the text, strip it; otherwise the
        // framework already removed it.
        if case let .paragraph(inlines)? = blocks.first, let prefix = taskCheckboxPrefix(inlines) {
            let stripped = strippingPrefix(prefix.length, from: inlines)
            if stripped.isEmpty {
                blocks.removeFirst()
            } else {
                blocks[0] = .paragraph(stripped)
            }
        }
        items.append(.task(isChecked: isChecked, blocks: blocks.isEmpty ? [.paragraph([])] : blocks))
    }
    return items
}

/// The checkbox state of a list item, read from its original source line, or
/// `nil` when the item is not a GFM task item (or its source range is unknown).
private func taskMarkerForItem(_ item: Markdown.ListItem, lines: [String]) -> Bool? {
    guard let line = item.range?.lowerBound.line, line >= 1, line <= lines.count else {
        return nil
    }
    var text = Substring(lines[line - 1])
    text = text.drop { $0 == " " || $0 == "\t" }
    // Strip the list marker: "- "/"* "/"+ " or an ordered marker "N."/"N)".
    if let first = text.first, first == "-" || first == "*" || first == "+" {
        text = text.dropFirst()
    } else {
        var i = text.startIndex
        while i < text.endIndex, text[i].isNumber { i = text.index(after: i) }
        if i > text.startIndex, i < text.endIndex, text[i] == "." || text[i] == ")" {
            text = text[text.index(after: i)...]
        } else {
            return nil
        }
    }
    text = text.drop { $0 == " " || $0 == "\t" }
    if text.hasPrefix("[ ] ") {
        return false
    }
    if text.hasPrefix("[x] ") || text.hasPrefix("[X] ") {
        return true
    }
    return nil
}

/// `(isChecked, markerLength)` when the inline sequence starts with a GFM
/// task-list marker.
private func taskCheckboxPrefix(_ inlines: [MarkdownInline]) -> (isChecked: Bool, length: Int)? {
    let text = plainText(of: inlines)
    if text.hasPrefix("[ ] ") {
        return (isChecked: false, length: 4)
    }
    if text.hasPrefix("[x] ") || text.hasPrefix("[X] ") {
        return (isChecked: true, length: 4)
    }
    return nil
}

/// Removes the first `count` characters from the concatenation of the leading
/// literal text runs (the task-list marker).
private func strippingPrefix(_ count: Int, from inlines: [MarkdownInline]) -> [MarkdownInline] {
    var remaining = count
    var result: [MarkdownInline] = []
    for inline in inlines {
        guard remaining > 0 else {
            result.append(inline)
            continue
        }
        switch inline {
        case let .text(text) where !text.isEmpty:
            let drop = min(text.count, remaining)
            remaining -= drop
            let kept = String(text.dropFirst(drop))
            if !kept.isEmpty {
                result.append(.text(kept))
            }
        case let .code(text) where !text.isEmpty:
            let drop = min(text.count, remaining)
            remaining -= drop
            let kept = String(text.dropFirst(drop))
            if !kept.isEmpty {
                result.append(.code(kept))
            }
        default:
            result.append(inline)
        }
    }
    return result
}

private func convertTable(_ table: Markdown.Table) -> MarkdownBlock {
    let headers: [[MarkdownInline]] = Array(table.head.children.map(convertCell))
    let rows: [[[MarkdownInline]]] = table.body.rows.map { row in Array(row.children.map(convertCell)) }
    // Pad rows so the renderer's Grid stays aligned to the header width.
    let width = max(headers.count, rows.map(\.count).max() ?? 0)
    let paddedHeaders = headers + Array(repeating: [.text("")] as [MarkdownInline], count: max(0, width - headers.count))
    let paddedRows = rows.map { row in
        row + Array(repeating: [.text("")] as [MarkdownInline], count: max(0, width - row.count))
    }
    return .table(MarkdownTable(headers: paddedHeaders, rows: paddedRows))
}

private func convertCell(_ markup: Markup) -> [MarkdownInline] {
    // A GFM cell is a small inline container; unwrap a nested paragraph.
    if let paragraph = markup as? Markdown.Paragraph {
        return convertInlines(paragraph.children)
    }
    return convertInlines(markup.children)
}

/// Converts a markup container's children into inline elements.
private func convertInlines(_ children: MarkupChildren) -> [MarkdownInline] {
    children.flatMap(convertInline)
}

private func convertInline(_ markup: Markup) -> [MarkdownInline] {
    if let text = markup as? Markdown.Text {
        return [.text(text.string)]
    }
    if let emphasis = markup as? Markdown.Emphasis {
        return [.emphasis(convertInlines(emphasis.children))]
    }
    if let strong = markup as? Markdown.Strong {
        return [.strong(convertInlines(strong.children))]
    }
    if let strike = markup as? Markdown.Strikethrough {
        return [.strikethrough(convertInlines(strike.children))]
    }
    if let code = markup as? Markdown.InlineCode {
        return [.code(code.code)]
    }
    if let link = markup as? Markdown.Link {
        return [.link(destination: link.destination, inlines: convertInlines(link.children))]
    }
    if let image = markup as? Markdown.Image {
        return [.image(source: image.source, alt: inlinePlainText(image.children))]
    }
    if let html = markup as? Markdown.InlineHTML {
        return [.rawHTML(html.rawHTML)]
    }
    if markup is Markdown.SoftBreak {
        return [.softBreak]
    }
    if markup is Markdown.LineBreak {
        return [.lineBreak]
    }
    // Unknown inline — degrade to its source text.
    return [.text(markup.format())]
}

/// The concatenated plain text of a set of inline elements (used for image
/// alt text and task-marker detection).
private func inlinePlainText(_ children: MarkupChildren) -> String {
    plainText(of: convertInlines(children))
}

private func plainText(of inlines: [MarkdownInline]) -> String {
    inlines.map { inline in
        switch inline {
        case let .text(text), let .code(text), let .rawHTML(text):
            text
        case let .emphasis(inlines), let .strong(inlines), let .strikethrough(inlines), let .link(_, inlines):
            plainText(of: inlines)
        case .image:
            ""
        case .softBreak, .lineBreak:
            " "
        }
    }.joined()
}
