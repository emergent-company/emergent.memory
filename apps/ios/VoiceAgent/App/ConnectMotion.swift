import SwiftUI

/// Reduce Motion choices for the connect flow (#1948).
///
/// Extracted from the view bodies so the decision is unit-testable: once
/// SwiftUI has resolved `@Environment(\.accessibilityReduceMotion)`, the
/// animation/transition a view ends up with cannot be read back from outside,
/// so a hosted layout test has nothing to inspect. `AccessibilityLayoutTests`
/// covers these functions; the call sites in `StartView`/`ConnectCTA` are still
/// review-only.
enum ConnectMotion {
    /// Implicit animation for the connect-flow content swap. `nil` means no
    /// animation at all, which is what Reduce Motion asks for.
    static func selectionAnimation(reduceMotion: Bool) -> Animation? {
        reduceMotion ? nil : Animation.default
    }

    /// Spinner entrance while connecting: scale + fade normally, fade only when
    /// Reduce Motion is on.
    static func busySpinnerTransition(reduceMotion: Bool) -> AnyTransition {
        busySpinnerScales(reduceMotion: reduceMotion)
            ? .scale.combined(with: .opacity)
            : .opacity
    }

    /// The testable half of ``busySpinnerTransition(reduceMotion:)``: whether
    /// the spinner is allowed to scale.
    static func busySpinnerScales(reduceMotion: Bool) -> Bool {
        !reduceMotion
    }
}
