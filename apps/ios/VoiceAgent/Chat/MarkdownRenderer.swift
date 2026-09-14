import SwiftUI

/// Renders agent markdown (parsed via ``parseMarkdown``) as a SwiftUI view
/// tree. Replaces the previous inline-only `AttributedString(markdown:)`.
///
/// This file must not `import Markdown`/`import Splash`: swift-markdown and
/// Splash declare top-level types (`Text`, `Image`, `Font`, ...) that shadow
/// SwiftUI's. Parsing/highlighting live in `MarkdownModel.swift` and
/// `MarkdownSyntaxHighlighter.swift`.
struct MarkdownRenderer: View {
    let text: String

    /// Base point size (matches the previous chat text size).
    static let baseFontSize: CGFloat = 15

    var body: some View {
        let blocks = parseMarkdown(text)
        if blocks.isEmpty {
            // Malformed/empty markdown degrades to plain text.
            Text(text)
                .font(.system(size: Self.baseFontSize))
        } else {
            renderBlocks(blocks, spacing: 2 * .grid)
        }
    }

    // MARK: Blocks

    @ViewBuilder
    private func renderBlocks(_ blocks: [MarkdownBlock], spacing: CGFloat) -> some View {
        VStack(alignment: .leading, spacing: spacing) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in
                renderBlock(block, spacing: spacing)
            }
        }
    }

    /// Renders one block. Returns `AnyView` to break the opaque-type recursion
    /// cycle between this view tree and ``renderBlocks`` (a `.blockQuote`/list
    /// contains more blocks).
    private func renderBlock(_ block: MarkdownBlock, spacing: CGFloat) -> AnyView {
        switch block {
        case let .heading(level, inlines):
            return AnyView(
                Text(attributedInlines(inlines))
                    .font(headingFont(level))
                    .foregroundStyle(.fg0)
                    .fixedSize(horizontal: false, vertical: true)
            )
        case let .paragraph(inlines):
            return AnyView(
                Text(attributedInlines(inlines))
                    .font(.system(size: Self.baseFontSize))
                    .foregroundStyle(.fg1)
                    .fixedSize(horizontal: false, vertical: true)
            )
        case let .codeBlock(language, code):
            return AnyView(codeBlockView(language: language, code: code))
        case let .blockQuote(blocks):
            return AnyView(
                HStack(alignment: .top, spacing: 2 * .grid) {
                    RoundedRectangle(cornerRadius: 1)
                        .fill(.fg3.opacity(0.5))
                        .frame(width: 3)
                    renderBlocks(blocks, spacing: 1 * .grid)
                        .padding(.vertical, 1)
                }
            )
        case let .list(isOrdered, startIndex, items):
            return AnyView(
                VStack(alignment: .leading, spacing: 1 * .grid) {
                    ForEach(Array(items.enumerated()), id: \.offset) { index, item in
                        listItemView(item, isOrdered: isOrdered, startIndex: startIndex, offset: index)
                    }
                }
            )
        case let .table(table):
            return AnyView(tableView(table))
        case let .image(source, alt):
            return AnyView(imageView(source: source, alt: alt))
        case .thematicBreak:
            return AnyView(
                Divider()
                    .overlay(.fg3.opacity(0.4))
            )
        }
    }

    private func listItemView(_ item: MarkdownListItem, isOrdered: Bool, startIndex: Int, offset: Int) -> AnyView {
        switch item {
        case let .task(isChecked, blocks):
            return AnyView(
                HStack(alignment: .top, spacing: 2 * .grid) {
                    Image(systemName: isChecked ? "checkmark.circle.fill" : "circle")
                        .font(.system(size: 14))
                        .foregroundStyle(isChecked ? .fgSuccess : .fg3)
                        .padding(.top, 1)
                    renderBlocks(blocks, spacing: 1 * .grid)
                }
            )
        case let .plain(blocks):
            return AnyView(
                HStack(alignment: .top, spacing: 2 * .grid) {
                    Text(isOrdered ? "\(startIndex + offset)." : "•")
                        .font(.system(size: Self.baseFontSize, weight: .medium))
                        .foregroundStyle(.fg2)
                    renderBlocks(blocks, spacing: 1 * .grid)
                }
            )
        }
    }

    private func headingFont(_ level: Int) -> Font {
        switch min(max(level, 1), 6) {
        case 1: .system(size: 22, weight: .bold)
        case 2: .system(size: 19, weight: .bold)
        case 3: .system(size: 17, weight: .semibold)
        case 4: .system(size: Self.baseFontSize + 1, weight: .semibold)
        default: .system(size: Self.baseFontSize, weight: .semibold)
        }
    }

    @ViewBuilder
    private func imageView(source: String?, alt: String?) -> some View {
        let fallback = Text(alt?.isEmpty == false ? alt! : "")
            .font(.system(size: 12))
            .foregroundStyle(.fg3)
        if let source,
           let url = URL(string: source),
           url.scheme == "http" || url.scheme == "https" {
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

    @ViewBuilder
    private func codeBlockView(language: String?, code: String) -> some View {
        VStack(alignment: .leading, spacing: 1 * .grid) {
            if let language, !language.isEmpty {
                Text(language)
                    .font(.system(size: 10, weight: .semibold, design: .monospaced))
                    .foregroundStyle(.fg3)
            }
            ScrollView(.horizontal, showsIndicators: false) {
                if let highlighted = MarkdownSyntaxHighlighter.highlight(code, language: language) {
                    Text(AttributedString(highlighted))
                        .textSelection(.enabled)
                } else {
                    // Highlight failure fallback: plain monospaced.
                    Text(code)
                        .font(.system(size: 13, design: .monospaced))
                        .foregroundStyle(.white)
                        .textSelection(.enabled)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(2 * .grid)
            .background(Color(white: 0.11), in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
        }
        .padding(.top, 1 * .grid)
    }

    // MARK: Tables

    @ViewBuilder
    private func tableView(_ table: MarkdownTable) -> some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Grid(alignment: .leading, horizontalSpacing: 2 * .grid, verticalSpacing: 1 * .grid) {
                GridRow {
                    ForEach(Array(table.headers.enumerated()), id: \.offset) { _, header in
                        Text(attributedInlines(header))
                            .font(.system(size: 13, weight: .semibold))
                            .foregroundStyle(.fg0)
                            .padding(1 * .grid)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(.bg2, in: RoundedRectangle(cornerRadius: 2))
                    }
                }
                ForEach(Array(table.rows.enumerated()), id: \.offset) { _, row in
                    GridRow {
                        ForEach(Array(row.enumerated()), id: \.offset) { _, cell in
                            Text(attributedInlines(cell))
                                .font(.system(size: 13))
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

    // MARK: Inline text

    /// Builds the styled attributed text for a sequence of inline elements.
    /// Emphasis/strong/code/strikethrough/links become rich-text runs inside
    /// one `AttributedString` so prose still wraps naturally.
    private func attributedInlines(_ inlines: [MarkdownInline]) -> AttributedString {
        makeAttributed(inlines, style: InlineStyle())
    }

    private struct InlineStyle {
        var isBold = false
        var isItalic = false
        var isStrikethrough = false
        var isCode = false
    }

    private func makeAttributed(_ inlines: [MarkdownInline], style: InlineStyle) -> AttributedString {
        var result = AttributedString()
        for inline in inlines {
            result.append(attributedInline(inline, style: style))
        }
        return result
    }

    private func attributedInline(_ inline: MarkdownInline, style: InlineStyle) -> AttributedString {
        switch inline {
        case let .text(text), let .rawHTML(text):
            return styledText(text, style: style)
        case .softBreak:
            return AttributedString(" ")
        case .lineBreak:
            return AttributedString("\n")
        case let .emphasis(inlines):
            var nested = style
            nested.isItalic = true
            return makeAttributed(inlines, style: nested)
        case let .strong(inlines):
            var nested = style
            nested.isBold = true
            return makeAttributed(inlines, style: nested)
        case let .strikethrough(inlines):
            var nested = style
            nested.isStrikethrough = true
            return makeAttributed(inlines, style: nested)
        case let .code(code):
            var nested = style
            nested.isCode = true
            return styledText(code, style: nested)
        case let .link(destination, inlines):
            var link = makeAttributed(inlines, style: style)
            if let destination, let url = URL(string: destination) {
                link[link.startIndex ..< link.endIndex].link = url
            }
            return link
        case let .image(source, alt):
            // Inline images inside prose render as their alt text (a
            // block-level image renders as a real AsyncImage).
            if let alt, !alt.isEmpty {
                return styledText(alt, style: style)
            }
            if let source {
                return styledText(source, style: style)
            }
            return AttributedString()
        }
    }

    private func styledText(_ text: String, style: InlineStyle) -> AttributedString {
        guard !text.isEmpty else { return AttributedString() }
        var run = AttributedString(text)
        if style.isCode {
            run.font = .system(size: Self.baseFontSize - 1, design: .monospaced)
            run.backgroundColor = .bg3
        } else {
            var font = Font.system(size: Self.baseFontSize)
            if style.isBold { font = font.weight(.semibold) }
            if style.isItalic { font = font.italic() }
            run.font = font
        }
        if style.isStrikethrough {
            run.strikethroughStyle = .single
        }
        return run
    }
}
