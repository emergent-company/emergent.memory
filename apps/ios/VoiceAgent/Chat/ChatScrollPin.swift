import SwiftUI

/// Whether a chat transcript is pinned to its newest row, and what a change of
/// geometry should do about it.
///
/// A chat follows its newest row only while the reader is already there. The
/// opposite rule — jump to the bottom on every token and every activity change —
/// made a reply impossible to read in the middle: a drag was undone by the next
/// event (`emergent.memory#434`), and when the last growth arrived after the
/// last scroll the landing stayed short of it, leaving the tail of the reply
/// outside the viewport (`emergent.memory#446`).
///
/// Both decisions are pure functions of two measurements, so they are pinned by
/// tests without a scroll view.
enum ChatScrollPin {
    /// How far above the content's end still counts as "at the bottom".
    ///
    /// A few points of slack, because a drag does not land exactly on the edge
    /// and a rubber-banded overscroll must not unpin the reader.
    static let threshold: CGFloat = 24

    /// What the decision is made from, in content coordinates.
    ///
    /// `viewportHeight` is here because the reader can be left behind by the
    /// viewport moving rather than the content growing — the keyboard leaving on
    /// send, or a composer that changes height.
    struct Measurement: Equatable {
        var visibleBottom: CGFloat
        var contentHeight: CGFloat
        var viewportHeight: CGFloat
    }

    static func isPinned(
        visibleBottom: CGFloat,
        contentHeight: CGFloat,
        threshold: CGFloat = threshold
    ) -> Bool {
        // Content shorter than the viewport has no bottom to be away from.
        visibleBottom >= contentHeight - threshold
    }

    /// Whether a change of geometry should move the transcript to its newest row.
    ///
    /// Two conditions, and both used to be missing:
    /// - only a change of size can leave a row outside the viewport, so a plain
    ///   scroll must never move the view — re-scrolling on it is what undid the
    ///   reader's drag;
    /// - the pin is read from the geometry *before* the change, because growth
    ///   moves the bottom away from the reader: judging the new geometry would
    ///   read "the reader is 200 pt from the bottom" as "they scrolled away",
    ///   and the view would stop following the moment a bubble grew past the
    ///   slack.
    static func shouldFollow(previous: Measurement, current: Measurement) -> Bool {
        let resized = previous.contentHeight != current.contentHeight
            || previous.viewportHeight != current.viewportHeight
        guard resized else { return false }
        return isPinned(
            visibleBottom: previous.visibleBottom,
            contentHeight: previous.contentHeight
        )
    }
}

/// Follows the newest row of a chat while the reader is at the bottom, and
/// leaves the view alone while they are reading back.
///
/// Applies to the `ScrollView` inside a `ScrollViewReader`, which supplies the
/// proxy. The scroll view's initial position is its own business
/// (`defaultScrollAnchor(_:for: .initialOffset)`); everything after that is
/// driven by ``ChatScrollPin/shouldFollow(previous:current:)``.
struct ChatAutoScroll: ViewModifier {
    let proxy: ScrollViewProxy
    /// The zero-height sentinel at the end of the content.
    let bottomAnchorID: String

    func body(content: Content) -> some View {
        content
            // A chat must never open on its oldest message. The initial anchor
            // covers a transcript that is already populated; this covers content
            // that lands before that anchor is applied.
            .onAppear {
                proxy.scrollTo(bottomAnchorID, anchor: .bottom)
            }
            .onScrollGeometryChange(for: ChatScrollPin.Measurement.self, of: Self.measure) { previous, current in
                guard ChatScrollPin.shouldFollow(previous: previous, current: current) else { return }
                proxy.scrollTo(bottomAnchorID, anchor: .bottom)
            }
    }

    private static func measure(_ geometry: ScrollGeometry) -> ChatScrollPin.Measurement {
        ChatScrollPin.Measurement(
            visibleBottom: geometry.visibleRect.maxY,
            contentHeight: geometry.contentSize.height,
            viewportHeight: geometry.containerSize.height
        )
    }
}
