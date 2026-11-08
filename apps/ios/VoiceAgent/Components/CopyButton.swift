import Foundation
import Observation
import SwiftUI

#if canImport(UIKit)
    import UIKit
#endif

/// The copy → checkmark → copy-glyph cycle, held apart from the view so the
/// 1.4-second window can be driven by a test instead of a wall clock.
///
/// Mirrors the web's copy buttons (`makeCopyButton`, `chat-components.js`): the
/// icon swaps to a check for a moment and then back, with no toast and no
/// layout shift. A second tap inside the window restarts it — the first wait
/// must not clear the feedback a newer tap just raised, which is what
/// ``generation`` guards.
@MainActor
@Observable
final class CopyFeedback {
    /// How long the checkmark stays up. The web uses 1400 ms.
    var duration: Duration
    /// Injected in tests; the shipped one waits without blocking a thread.
    var sleep: (Duration) async -> Void

    /// `true` while the checkmark is showing.
    private(set) var isCopied = false
    /// Bumped on every tap; a finished wait only clears its own generation.
    private(set) var generation = 0

    init(
        duration: Duration = .seconds(1.4),
        sleep: @escaping (Duration) async -> Void = { try? await Task.sleep(for: $0) }
    ) {
        self.duration = duration
        self.sleep = sleep
    }

    /// Writes `text` and starts (or restarts) the confirmation window.
    ///
    /// The task is returned so a test can await it; the view discards it.
    @discardableResult
    func copy(_ text: String, write: (String) -> Void) -> Task<Void, Never> {
        write(text)
        generation &+= 1
        let token = generation
        isCopied = true
        return Task { [weak self] in
            guard let self else { return }
            await sleep(duration)
            // A tap that landed inside this window took a newer token: its
            // checkmark must outlive the older wait.
            guard generation == token else { return }
            isCopied = false
        }
    }
}

/// The app's one copy affordance: copies `text()`, swaps the glyph for a
/// checkmark for 1.4 s, and announces "Copied" while it shows.
///
/// One component for every copy control — an object card's id, a fenced code
/// block, a reply's action row, a turn's footer — so the confirmation, the
/// haptic and the accessibility behaviour are identical everywhere.
struct CopyButton: View {
    /// What goes on the pasteboard. Evaluated on tap, so a reply that is still
    /// streaming copies the text as it stands then.
    let text: () -> String
    /// The control's name while idle ("Copy message", "Copy code", ...).
    let label: LocalizedStringKey
    /// Show `label` beside the glyph instead of the glyph alone.
    var showsLabel = false
    /// The name announced while the checkmark shows.
    var copiedLabel: LocalizedStringKey = "chat.copy.done"
    /// Injected in tests in place of the singleton pasteboard.
    var write: (String) -> Void = CopyButton.writeToPasteboard

    @State private var feedback: CopyFeedback

    init(
        text: @escaping () -> String,
        label: LocalizedStringKey,
        showsLabel: Bool = false,
        copiedLabel: LocalizedStringKey = "chat.copy.done",
        write: @escaping (String) -> Void = CopyButton.writeToPasteboard,
        duration: Duration = .seconds(1.4),
        sleep: @escaping (Duration) async -> Void = { try? await Task.sleep(for: $0) }
    ) {
        self.text = text
        self.label = label
        self.showsLabel = showsLabel
        self.copiedLabel = copiedLabel
        self.write = write
        _feedback = State(initialValue: CopyFeedback(duration: duration, sleep: sleep))
    }

    var body: some View {
        Button {
            feedback.copy(text(), write: write)
        } label: {
            HStack(spacing: 1 * .grid) {
                Image(systemName: feedback.isCopied ? "checkmark" : "doc.on.doc")
                if showsLabel {
                    Text(feedback.isCopied ? copiedLabel : label)
                }
            }
            .font(showsLabel ? MemoryFont.label : .footnote)
            .foregroundStyle(feedback.isCopied ? .fgSuccess : .fg3)
            .padding(.horizontal, showsLabel ? 1.5 * .grid : 0)
            .padding(.vertical, showsLabel ? 1 * .grid : 0)
            .frame(minWidth: showsLabel ? 0 : 44, minHeight: showsLabel ? 32 : 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        #if os(iOS)
            .sensoryFeedback(.success, trigger: feedback.isCopied) { _, now in now }
        #endif
            .accessibilityLabel(Text(feedback.isCopied ? copiedLabel : label))
    }

    /// The real pasteboard write. A seam, not a singleton call, so a test can
    /// assert the copy action without the simulator's shared pasteboard.
    static var writeToPasteboard: (String) -> Void {
        { text in
            #if canImport(UIKit)
                UIPasteboard.general.string = text
            #endif
        }
    }
}
