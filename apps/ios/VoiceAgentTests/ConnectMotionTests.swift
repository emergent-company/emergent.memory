@testable import Memory
import Testing

/// Covers the Reduce Motion choices the connect flow makes (#1948).
///
/// Once SwiftUI has resolved `@Environment(\.accessibilityReduceMotion)`, a
/// hosted view exposes neither the animation nor the transition it applied, so
/// the decision lives in `ConnectMotion` where it can be asserted directly.
/// What these tests cannot prove is that the call sites in `StartView` /
/// `ConnectCTA` keep consulting it — that stays code review.
struct ConnectMotionTests {
    @Test(arguments: [true, false])
    func selectionAnimationFollowsReduceMotion(reduceMotion: Bool) {
        let animation = ConnectMotion.selectionAnimation(reduceMotion: reduceMotion)
        #expect((animation == nil) == reduceMotion)
    }

    @Test(arguments: [true, false])
    func busySpinnerScalesOnlyWithoutReduceMotion(reduceMotion: Bool) {
        #expect(ConnectMotion.busySpinnerScales(reduceMotion: reduceMotion) == !reduceMotion)
    }
}
