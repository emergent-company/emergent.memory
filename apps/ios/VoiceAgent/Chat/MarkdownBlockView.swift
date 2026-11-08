import SwiftUI

/// One block of a parsed agent markdown document.
///
/// A concrete struct rather than `AnyView`: a long reply is a `ForEach` of
/// these, so once a block is committed SwiftUI can diff it instead of
/// rebuilding the whole tree on every streamed token (issue #441 item 3).
///
/// This file must not `import Markdown`/`import Splash`: swift-markdown and
/// Splash declare top-level types (`Text`, `Image`, `Font`, ...) that shadow
/// SwiftUI's. Parsing/highlighting live in `MarkdownModel.swift`,
/// `StreamingMarkdownModel.swift` and `MarkdownSyntaxHighlighter.swift`.
struct MarkdownBlockView: View {
    let block: MarkdownBlock
    /// Fenced code is highlighted only once its block is committed; the
    /// still-streaming tail stays plain monospaced (issue #441 item 5).
    var highlightsCode = true

    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        switch block {
        case let .heading(level, inlines):
            Text(MarkdownInlineStyling.attributed(inlines))
                .font(MarkdownTypography.headingFont(level))
                .foregroundStyle(.fg0)
                .fixedSize(horizontal: false, vertical: true)
        case let .paragraph(inlines):
            Text(MarkdownInlineStyling.attributed(inlines))
                .font(MarkdownTypography.baseFont)
                .foregroundStyle(.fg1)
                .fixedSize(horizontal: false, vertical: true)
        case let .codeBlock(language, code):
            codeBlockView(language: language, code: code)
        case let .blockQuote(blocks):
            blockQuoteView(blocks)
        case let .list(isOrdered, startIndex, items):
            listView(items, isOrdered: isOrdered, startIndex: startIndex)
        case let .table(model):
            tableView(model)
        case let .image(source, alt):
            imageView(source: source, alt: alt)
        case .thematicBreak:
            Divider()
                .overlay(.fg3.opacity(0.4))
        }
    }

    // MARK: Containers

    private func blockQuoteView(_ blocks: [MarkdownBlock]) -> some View {
        HStack(alignment: .top, spacing: 2 * .grid) {
            RoundedRectangle(cornerRadius: 1)
                .fill(.fg3.opacity(0.5))
                .frame(width: 3)
            MarkdownBlocksView(blocks: blocks, highlightsCode: highlightsCode, spacing: 1 * .grid)
                .padding(.vertical, 1)
        }
    }

    private func listView(
        _ items: [MarkdownListItem],
        isOrdered: Bool,
        startIndex: Int
    ) -> some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            ForEach(Array(items.enumerated()), id: \.offset) { offset, item in
                listItemView(item, isOrdered: isOrdered, startIndex: startIndex, offset: offset)
            }
        }
    }

    @ViewBuilder
    private func listItemView(
        _ item: MarkdownListItem,
        isOrdered: Bool,
        startIndex: Int,
        offset: Int
    ) -> some View {
        switch item {
        case let .task(isChecked, blocks):
            HStack(alignment: .top, spacing: 2 * .grid) {
                Image(systemName: isChecked ? "checkmark.circle.fill" : "circle")
                    .font(.callout)
                    .foregroundStyle(isChecked ? .fgSuccess : .fg3)
                    .padding(.top, 1)
                MarkdownBlocksView(blocks: blocks, highlightsCode: highlightsCode, spacing: 1 * .grid)
            }
        case let .plain(blocks):
            HStack(alignment: .top, spacing: 2 * .grid) {
                Text(isOrdered ? "\(startIndex + offset)." : "•")
                    .font(MarkdownTypography.baseFont.weight(.medium))
                    .foregroundStyle(.fg2)
                MarkdownBlocksView(blocks: blocks, highlightsCode: highlightsCode, spacing: 1 * .grid)
            }
        }
    }

    // MARK: Images

    @ViewBuilder
    private func imageView(source: String?, alt: String?) -> some View {
        let fallback = Text(alt?.isEmpty == false ? alt! : "")
            .font(MemoryFont.meta)
            .foregroundStyle(.fg3)
        if let source,
           let url = URL(string: source),
           url.scheme == "http" || url.scheme == "https"
        {
            AsyncImage(url: url) { phase in
                switch phase {
                case let .success(image):
                    image
                        .resizable()
                        .scaledToFit()
                        .clipShape(RoundedRectangle(cornerRadius: .cornerRadiusSmall))
                case .failure:
                    fallback
                case .empty:
                    ProgressView()
                        .controlSize(.small)
                        .frame(maxWidth: .infinity)
                @unknown default:
                    fallback
                }
            }
            .frame(maxWidth: .infinity)
        } else {
            fallback
        }
    }

    // MARK: Code blocks

    private func codeBlockView(language: String?, code: String) -> some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            if let language, !language.isEmpty {
                Text(language)
                    .font(.system(.caption2, design: .monospaced, weight: .semibold))
                    .foregroundStyle(.fg3)
            }
            ScrollView(.horizontal, showsIndicators: false) {
                if highlightsCode,
                   let highlighted = MarkdownSyntaxHighlighter.highlight(
                       code,
                       language: language,
                       isDark: colorScheme == .dark
                   )
                {
                    Text(AttributedString(highlighted))
                        .textSelection(.enabled)
                } else {
                    // Uncommitted code and highlight failures fall back to plain
                    // monospaced token text.
                    Text(code)
                        .font(.system(.footnote, design: .monospaced))
                        .foregroundStyle(.fg1)
                        .textSelection(.enabled)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(2 * .grid)
            .background(.codeBackground, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
        }
        .padding(.top, 1 * .grid)
    }

    // MARK: Tables

    private func tableView(_ table: MarkdownTable) -> some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Grid(alignment: .leading, horizontalSpacing: 2 * .grid, verticalSpacing: 1 * .grid) {
                GridRow {
                    ForEach(Array(table.headers.enumerated()), id: \.offset) { _, header in
                        Text(MarkdownInlineStyling.attributed(header))
                            .font(MemoryFont.labelEmphasis)
                            .foregroundStyle(.fg0)
                            .padding(1 * .grid)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(.bg2, in: RoundedRectangle(cornerRadius: 2))
                    }
                }
                ForEach(Array(table.rows.enumerated()), id: \.offset) { _, row in
                    GridRow {
                        ForEach(Array(row.enumerated()), id: \.offset) { _, cell in
                            Text(MarkdownInlineStyling.attributed(cell))
                                .font(MemoryFont.label)
                                .foregroundStyle(.fg1)
                                .padding(1 * .grid)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                    }
                }
            }
        }
        .overlay(
            RoundedRectangle(cornerRadius: .cornerRadiusSmall)
                .stroke(.separator1.opacity(0.4), lineWidth: 1)
        )
        .clipShape(RoundedRectangle(cornerRadius: .cornerRadiusSmall))
    }
}

// MARK: - Block stacks

/// A vertical stack of blocks, used for the document body, nested containers
/// and the streaming tail.
struct MarkdownBlocksView: View {
    let blocks: [MarkdownBlock]
    var highlightsCode = true
    var spacing: CGFloat = 2 * .grid

    var body: some View {
        VStack(alignment: .leading, spacing: spacing) {
            ForEach(Array(blocks.indices), id: \.self) { index in
                MarkdownBlockView(block: blocks[index], highlightsCode: highlightsCode)
            }
        }
    }
}

// MARK: - Typography

enum MarkdownTypography {
    /// Base prose font for the agent bubble; scales with Dynamic Type.
    static let baseFont: Font = MemoryFont.conversationBody
    /// Inline-code font.
    static let monoFont: Font = .system(.callout, design: .monospaced)

    static func headingFont(_ level: Int) -> Font {
        switch min(max(level, 1), 6) {
        case 1: .title2.weight(.bold)
        case 2: .title3.weight(.bold)
        case 3: .headline
        case 4: .callout.weight(.semibold)
        default: .body.weight(.semibold)
        }
    }
}

// MARK: - Inline text

/// Builds the styled attributed text for a sequence of inline elements.
/// Emphasis/strong/code/strikethrough/links become rich-text runs inside one
/// `AttributedString` so prose still wraps naturally.
enum MarkdownInlineStyling {
    static func attributed(_ inlines: [MarkdownInline]) -> AttributedString {
        makeAttributed(inlines, style: InlineStyle())
    }

    private struct InlineStyle {
        var isBold = false
        var isItalic = false
        var isStrikethrough = false
        var isCode = false
    }

    private static func makeAttributed(
        _ inlines: [MarkdownInline],
        style: InlineStyle
    ) -> AttributedString {
        var result = AttributedString()
        for inline in inlines {
            result.append(attributedInline(inline, style: style))
        }
        return result
    }

    private static func attributedInline(
        _ inline: MarkdownInline,
        style: InlineStyle
    ) -> AttributedString {
        switch inline {
        case let .text(text), let .rawHTML(text):
            styledText(text, style: style)
        case .softBreak:
            AttributedString(" ")
        case .lineBreak:
            AttributedString("\n")
        case let .code(code):
            styledText(code, style: decorated(style, code: true))
        default:
            styledContainer(inline, style: style)
        }
    }

    /// The nesting/decorating cases, split off to keep each dispatch small.
    private static func styledContainer(
        _ inline: MarkdownInline,
        style: InlineStyle
    ) -> AttributedString {
        switch inline {
        case let .emphasis(inlines):
            makeAttributed(inlines, style: decorated(style, italic: true))
        case let .strong(inlines):
            makeAttributed(inlines, style: decorated(style, bold: true))
        case let .strikethrough(inlines):
            makeAttributed(inlines, style: decorated(style, strike: true))
        case let .link(destination, inlines):
            linked(inlines, destination: destination, style: style)
        case let .image(source, alt):
            imageText(source: source, alt: alt, style: style)
        default:
            AttributedString()
        }
    }

    /// A copy of `style` with one more decoration applied.
    private static func decorated(
        _ style: InlineStyle,
        bold: Bool = false,
        italic: Bool = false,
        strike: Bool = false,
        code: Bool = false
    ) -> InlineStyle {
        var copy = style
        copy.isBold = copy.isBold || bold
        copy.isItalic = copy.isItalic || italic
        copy.isStrikethrough = copy.isStrikethrough || strike
        copy.isCode = copy.isCode || code
        return copy
    }

    private static func linked(
        _ inlines: [MarkdownInline],
        destination: String?,
        style: InlineStyle
    ) -> AttributedString {
        var link = makeAttributed(inlines, style: style)
        if let destination, let url = URL(string: destination) {
            link[link.startIndex ..< link.endIndex].link = url
        }
        return link
    }

    /// Inline images inside prose render as their alt text (a block-level
    /// image renders as a real `AsyncImage`).
    private static func imageText(
        source: String?,
        alt: String?,
        style: InlineStyle
    ) -> AttributedString {
        if let alt, !alt.isEmpty {
            return styledText(alt, style: style)
        }
        if let source {
            return styledText(source, style: style)
        }
        return AttributedString()
    }

    private static func styledText(_ text: String, style: InlineStyle) -> AttributedString {
        guard !text.isEmpty else { return AttributedString() }
        var run = AttributedString(text)
        if style.isCode {
            run.font = MarkdownTypography.monoFont
            run.backgroundColor = .codeBackground
        } else {
            var font = MarkdownTypography.baseFont
            if style.isBold {
                font = font.weight(.semibold)
            }
            if style.isItalic {
                font = font.italic()
            }
            run.font = font
        }
        if style.isStrikethrough {
            run.strikethroughStyle = .single
        }
        return run
    }
}
