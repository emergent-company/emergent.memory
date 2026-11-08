import SwiftUI

/// Whether a chat transcript is pinned to its newest row.
///
/// A chat follows its newest row only while the reader is already there. The
/// opposite rule — jump to the bottom on every token and every activity change —
/// made a reply impossible to read in the middle: a drag was undone by the next
/// event (`emergent.memory#434`), and when the last growth arrived after the
/// last scroll the landing stayed short of it, leaving the tail of the reply
/// outside the viewport (`emergent.memory#446`).
///
/// The decision is a pure function of two measurements so it can be tested
/// without a scroll view: the visible bottom edge and the content's height, both
/// in content coordinates.
enum ChatScrollPin {
    /// How far above the content's end still counts as "at the bottom".
    ///
    /// A few points of slack, because a drag does not land exactly on the edge
    /// and a rubber-banded overscroll must not unpin the reader.
    static let threshold: CGFloat = 24

    static func isPinned(
        visibleBottom: CGFloat,
        contentHeight: CGFloat,
        threshold: CGFloat = threshold
    ) -> Bool {
        // Content shorter than the viewport has no bottom to be away from.
        visibleBottom >= contentHeight - threshold
    }
}

/// Follows the newest row of a chat while the reader is at the bottom, and
/// leaves the view alone while they are reading back.
///
/// Applies to the `ScrollView` inside a `ScrollViewReader`, which supplies the
/// proxy. A change of size is what moves the view: a streamed token, a recorded
/// history load, a dismissing keyboard (the viewport grows), a composer that
/// changes height, or Markdown that settles at a height other than the row it
/// replaced.
struct ChatAutoScroll: ViewModifier {
    let proxy: ScrollViewProxy
    /// The zero-height sentinel at the end of the content.
    let bottomAnchorID: String

    @State private var isPinned = true
    /// The first measurement has no predecessor to compare against, and it is
    /// the one that lands the view on the newest row.
    @State private var hasMeasured = false

    func body(content: Content) -> some View {
        content
            // Land on the newest row on open, whatever the first measurement
            // reports: a chat must never open on the oldest message.
            .onAppear {
                proxy.scrollTo(bottomAnchorID, anchor: .bottom)
            }
            .onScrollGeometryChange(for: Measurements.self, of: Self.measure) { previous, current in
                isPinned = ChatScrollPin.isPinned(
                    visibleBottom: current.visibleBottom,
                    contentHeight: current.contentHeight
                )
                let firstMeasurement = !hasMeasured
                hasMeasured = true
                // Only a change of size can leave the newest row outside the
                // viewport. A plain scroll changes the offset alone, and
                // re-scrolling on it would fight the reader's own drag.
                guard isPinned,
                      firstMeasurement
                      || previous.contentHeight != current.contentHeight
                      || previous.viewportHeight != current.viewportHeight
                else { return }
                proxy.scrollTo(bottomAnchorID, anchor: .bottom)
            }
    }

    private static func measure(_ geometry: ScrollGeometry) -> Measurements {
        Measurements(
            visibleBottom: geometry.visibleRect.maxY,
            contentHeight: geometry.contentSize.height,
            viewportHeight: geometry.containerSize.height
        )
    }

    private struct Measurements: Equatable {
        var visibleBottom: CGFloat
        var contentHeight: CGFloat
        var viewportHeight: CGFloat
    }
}
