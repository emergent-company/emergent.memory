import Foundation
@testable import Memory
import Testing

/// The rule a chat follows its newest row by (`emergent.memory#434`,
/// `emergent.memory#446`).
///
/// Pure geometry, so it is pinned here rather than through a scroll view: both
/// bugs were about *when* the view moved — it moved while the reader was reading
/// back, and it stopped following as soon as the row that grew pushed the bottom
/// more than the slack away from them.
struct ChatScrollPinTests {
    private func state(
        contentHeight: CGFloat,
        visibleBottom: CGFloat,
        viewportHeight: CGFloat = 800
    ) -> ChatScrollPin.Measurement {
        ChatScrollPin.Measurement(
            visibleBottom: visibleBottom,
            contentHeight: contentHeight,
            viewportHeight: viewportHeight
        )
    }

    // MARK: - The pin

    @Test func theContentEndIsPinned() {
        #expect(ChatScrollPin.isPinned(visibleBottom: 1200, contentHeight: 1200))
        #expect(ChatScrollPin.isPinned(visibleBottom: 1190, contentHeight: 1200))
    }

    @Test func aReaderInsideTheSlackIsStillPinned() {
        // A drag or a rubber band does not land exactly on the edge.
        #expect(ChatScrollPin.isPinned(visibleBottom: 1176, contentHeight: 1200))
    }

    @Test func aReaderWhoScrolledBackIsNotPinned() {
        #expect(ChatScrollPin.isPinned(visibleBottom: 800, contentHeight: 1200) == false)
        // Just past the slack: the next token must not move the view.
        #expect(ChatScrollPin.isPinned(visibleBottom: 1175, contentHeight: 1200) == false)
    }

    @Test func contentShorterThanTheViewportIsPinned() {
        // Nothing is hidden below, so there is no bottom to be away from.
        #expect(ChatScrollPin.isPinned(visibleBottom: 600, contentHeight: 200))
    }

    @Test func theThresholdIsSettable() {
        #expect(ChatScrollPin.isPinned(visibleBottom: 1000, contentHeight: 1200, threshold: 300))
        #expect(ChatScrollPin.isPinned(visibleBottom: 1000, contentHeight: 1200, threshold: 100) == false)
    }

    // MARK: - Following

    @Test func growthWhileTheReaderIsAtTheBottomIsFollowed() {
        // 1200 → 1400: the reader is 200 pt from the new bottom, far outside the
        // slack. Judging the *new* geometry would read that as "scrolled away"
        // and stop following for good, which is exactly the #446 regression.
        let before = state(contentHeight: 1200, visibleBottom: 1200)
        let after = state(contentHeight: 1400, visibleBottom: 1200)
        #expect(ChatScrollPin.shouldFollow(previous: before, current: after))
    }

    @Test func growthWhileTheReaderIsReadingBackIsNotFollowed() {
        let before = state(contentHeight: 1200, visibleBottom: 800)
        let after = state(contentHeight: 1400, visibleBottom: 800)
        #expect(ChatScrollPin.shouldFollow(previous: before, current: after) == false)
    }

    @Test func aPlainScrollNeverMovesTheView() {
        // Offset alone: the reader is dragging, and nothing about the content
        // changed. This is the #434 hijack condition.
        let before = state(contentHeight: 1200, visibleBottom: 800)
        let after = state(contentHeight: 1200, visibleBottom: 1000)
        #expect(ChatScrollPin.shouldFollow(previous: before, current: after) == false)
        #expect(ChatScrollPin.shouldFollow(previous: after, current: after) == false)
    }

    @Test func aViewportThatShrinksWhileAtTheBottomIsFollowed() {
        // The keyboard goes away on send: the viewport grows, so the newest row
        // must be brought back into it even though the content did not change.
        let before = state(contentHeight: 1200, visibleBottom: 1200, viewportHeight: 500)
        let after = state(contentHeight: 1200, visibleBottom: 1200, viewportHeight: 800)
        #expect(ChatScrollPin.shouldFollow(previous: before, current: after))
    }

    @Test func aViewportThatChangesWhileReadingBackIsNotFollowed() {
        let before = state(contentHeight: 1200, visibleBottom: 700, viewportHeight: 500)
        let after = state(contentHeight: 1200, visibleBottom: 700, viewportHeight: 800)
        #expect(ChatScrollPin.shouldFollow(previous: before, current: after) == false)
    }

    @Test func theFirstMeasurementFollowsNothing() {
        // `previous` and `current` are the same value on the first report, and the
        // initial position is the scroll view's anchor, not this rule's job.
        let first = state(contentHeight: 1200, visibleBottom: 800)
        #expect(ChatScrollPin.shouldFollow(previous: first, current: first) == false)
    }
}
