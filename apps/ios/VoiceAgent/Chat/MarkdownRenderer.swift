import SwiftUI

/// Renders agent markdown (parsed via ``parseMarkdown``) as a SwiftUI view
/// tree. Replaces the previous inline-only `AttributedString(markdown:)`.
///
/// The renderer keeps an incremental parse (``StreamingMarkdownModel``) in
/// `@State`, so a streamed reply only re-parses the block it is still typing
/// and finished blocks are diffed instead of rebuilt (issue #441). The call
/// sites are unchanged: whether the text is still growing is derived here from
/// the text itself, so history and live replies share one entry point.
///
/// This file must not `import Markdown`/`import Splash`: swift-markdown and
/// Splash declare top-level types (`Text`, `Image`, `Font`, ...) that shadow
/// SwiftUI's. Parsing/highlighting live in `MarkdownModel.swift`,
/// `StreamingMarkdownModel.swift` and `MarkdownSyntaxHighlighter.swift`; the
/// block and inline views live in `MarkdownBlockView.swift`.
struct MarkdownRenderer: View {
    let text: String

    /// Incremental parse state, kept across `body` evaluations.
    @State private var cache = MarkdownRenderCache()
    /// The authoritative one-shot parse written once streaming goes quiet.
    @State private var settled: SettledMarkdown?

    var body: some View {
        content
            .task(id: text) { await settleWhenQuiet() }
    }

    @ViewBuilder
    private var content: some View {
        let snapshot = snapshot()
        if snapshot.isEmpty {
            // Malformed/empty markdown degrades to plain text.
            Text(text)
                .font(MarkdownTypography.baseFont)
        } else {
            VStack(alignment: .leading, spacing: 2 * .grid) {
                // A committed block's id is its index, which never changes
                // once committed, so SwiftUI diffs the finished part of the
                // answer instead of rebuilding it on every token.
                ForEach(Array(snapshot.committed.indices), id: \.self) { index in
                    MarkdownBlockView(block: snapshot.committed[index])
                }
                if !snapshot.tailBlocks.isEmpty {
                    // The moving part of the message is one element with a
                    // fixed id, re-parsed per delta (O(tail), not O(reply)).
                    MarkdownBlocksView(blocks: snapshot.tailBlocks, highlightsCode: false)
                        .id("tail")
                }
            }
        }
    }

    private func snapshot() -> MarkdownRenderSnapshot {
        if let settled, settled.text == text {
            return MarkdownRenderSnapshot(committed: settled.blocks, tailBlocks: [])
        }
        return cache.snapshot(for: text)
    }

    /// Streaming only optimises the intermediate frames: after a short quiet
    /// period the message is parsed once as a whole, so the last block ends up
    /// styled exactly like a history message (highlighting included) instead of
    /// staying in its unfinished tail form.
    private func settleWhenQuiet() async {
        guard cache.isStreaming else { return }
        try? await Task.sleep(for: .milliseconds(350))
        guard !Task.isCancelled, cache.isStreaming else { return }
        settled = SettledMarkdown(text: text, blocks: cache.finish())
    }
}

/// The result of settling: the whole message parsed once as a document.
struct SettledMarkdown {
    let text: String
    let blocks: [MarkdownBlock]
}

/// What a renderer draws for one text: the frozen blocks and the tail.
struct MarkdownRenderSnapshot {
    var committed: [MarkdownBlock] = []
    var tailBlocks: [MarkdownBlock] = []

    var isEmpty: Bool { committed.isEmpty && tailBlocks.isEmpty }
}

/// Keeps the incremental parse alive across `body` evaluations.
///
/// A reference type so `@State` can hold it: mutating it never invalidates the
/// view (a delta arrives with a new `text`, which re-renders anyway), and the
/// snapshot for a given text is stable, so `body` stays free of side effects
/// from SwiftUI's point of view.
final class MarkdownRenderCache {
    private var model = StreamingMarkdownModel()
    private var lastText = ""
    private var staticText: String?
    private var staticBlocks: [MarkdownBlock] = []
    private var isStreamingMessage = false

    /// `true` once this message has been observed growing (i.e. it streams).
    var isStreaming: Bool { isStreamingMessage }

    func snapshot(for text: String) -> MarkdownRenderSnapshot {
        if !isStreamingMessage, grew(from: lastText, to: text) {
            isStreamingMessage = true
        }
        lastText = text
        guard isStreamingMessage else {
            // History / ended turns: parse once, memoized by text identity.
            if staticText != text {
                staticText = text
                staticBlocks = parseMarkdown(text)
            }
            return MarkdownRenderSnapshot(committed: staticBlocks, tailBlocks: [])
        }
        model.update(text)
        return MarkdownRenderSnapshot(committed: model.committed, tailBlocks: model.tailBlocks)
    }

    /// Parses the whole message once and returns its blocks.
    func finish() -> [MarkdownBlock] {
        model.finish()
        return model.committed
    }

    /// Growth is only meaningful after a first observation: a view that appears
    /// with its whole text (history, an ended turn) is not a stream.
    private func grew(from previous: String, to text: String) -> Bool {
        !previous.isEmpty && text.utf8.count > previous.utf8.count && text.hasPrefix(previous)
    }
}
