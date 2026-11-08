import Foundation
@testable import Memory
import Testing

/// The rule a chat follows its newest row by (`emergent.memory#434`).
///
/// Pure geometry, so it is pinned here rather than through a scroll view: the
/// two bugs it fixes were both about *when* the view moved — it moved while the
/// reader was reading back, and it landed short of the row that had just grown.
struct ChatScrollPinTests {
    @Test func theContentEndIsPinned() {
        #expect(ChatScrollPin.isPinned(visibleBottom: 1200, contentHeight: 1200))
        #expect(ChatScrollPin.isPinned(visibleBottom: 1190, contentHeight: 1200))
    }

    @Test func aReaderInsideTheHalfPointIsStillPinned() {
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
}
