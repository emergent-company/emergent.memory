@testable import Memory
import SwiftUI
import Testing
import UIKit

/// Regression coverage for the accessibility text-size defects from #1948
/// (fixed in #1953). `ios-test` otherwise only proves the app compiles — the
/// app has no UI-test target, so view layout is never exercised.
///
/// Each case hosts the production view in a `UIHostingController`, sets
/// `\.dynamicTypeSize`, and measures the laid-out size, so a return to a fixed
/// frame or an unconditional `lineLimit(1)` fails on rendered geometry rather
/// than on a grep of the source.
@MainActor
struct AccessibilityLayoutTests {
    /// The connect CTA's documented minimum footprint (`11 * .grid`, 4pt grid).
    private let ctaMinimumHeight: CGFloat = 44
    /// A modern phone width, so the CTA is hosted at its usual full width.
    private let phoneWidth: CGFloat = 390
    /// Narrower than a phone's content width, so a long header title must wrap.
    private let headerWidth: CGFloat = 320

    private let shortTitle = Text(verbatim: "Agents")
    private let longTitle = Text(verbatim: "Weekly retrieval quality review for the support agent")

    // MARK: - Connect CTA (#1948)

    /// The CTA used to be framed at a fixed 232×44pt, so at accessibility sizes
    /// the label was clipped instead of growing the button. It must measure
    /// taller than its 44pt minimum when the text is at `.accessibility5`.
    @Test func connectCTAGrowsAtAccessibilityTextSizes() {
        let atDefault = height(of: ConnectCTA(action: {}), width: phoneWidth, dynamicTypeSize: .large)
        let atAccessibility5 = height(of: ConnectCTA(action: {}), width: phoneWidth, dynamicTypeSize: .accessibility5)

        // Sanity: at default sizes the footprint is still the designed pill, so
        // the accessibility height is growth rather than a shifted design.
        #expect(atDefault <= ctaMinimumHeight + 1)
        #expect(atAccessibility5 > ctaMinimumHeight + 1)
    }

    // MARK: - Screen header (#1948)

    /// The header title was pinned to one line, truncating the only cue for
    /// which pushed screen is showing. At accessibility sizes it must reach a
    /// second line, which makes the header taller than a one-line title.
    @Test func screenHeaderTitleWrapsAtAccessibilityTextSizes() {
        let oneLine = headerHeight(shortTitle, dynamicTypeSize: .accessibility5)
        let longTitleHeight = headerHeight(longTitle, dynamicTypeSize: .accessibility5)

        // One extra line of the title3 ramp at `.accessibility5` is far taller
        // than this margin; a single-line title would measure the same height.
        #expect(longTitleHeight - oneLine > 20)
    }

    /// The single-line rule stays for default text sizes: a long title must not
    /// silently start wrapping on ordinary phones.
    @Test func screenHeaderTitleStaysSingleLineAtDefaultTextSizes() {
        let oneLine = headerHeight(shortTitle, dynamicTypeSize: .large)
        let longTitleHeight = headerHeight(longTitle, dynamicTypeSize: .large)

        #expect(abs(longTitleHeight - oneLine) < 1)
    }

    // MARK: - Hosting

    private func headerHeight(_ title: Text, dynamicTypeSize: DynamicTypeSize) -> CGFloat {
        height(
            of: ScreenHeader(title: title, subtitle: nil, onBack: {}),
            width: headerWidth,
            dynamicTypeSize: dynamicTypeSize
        )
    }

    /// Runs a real SwiftUI layout pass for `view` at `width` with an unbounded
    /// height proposal, so fonts, frames and line limits all take effect.
    private func height(of view: some View, width: CGFloat, dynamicTypeSize: DynamicTypeSize) -> CGFloat {
        let host = UIHostingController(rootView: view.environment(\.dynamicTypeSize, dynamicTypeSize))
        _ = host.view // load the backing view before measuring
        let fitted = host.sizeThatFits(in: CGSize(width: width, height: .greatestFiniteMagnitude))
        #expect(
            fitted.height > 0 && fitted.height < 10000,
            Comment(rawValue: "unexpected laid-out height \(fitted.height) at width \(width)")
        )
        return fitted.height
    }
}
