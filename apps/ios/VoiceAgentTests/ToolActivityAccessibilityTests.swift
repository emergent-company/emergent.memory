import Foundation
@testable import Memory
import SwiftUI
import Testing

// MARK: - Summary accessibility contract

/// The summary line is the only way into a closed run's rows, so what it says
/// to assistive technology is part of the feature, not decoration.
///
/// SwiftUI does not vend its accessibility tree to a `UIHostingController` in
/// this test target, so the view projects `SummaryAccessibility` one-to-one, as
/// it does `linePieces`: one element, labelled with the count/span/failure and
/// valued with the disclosure state. The glyph strip, failure glyph and chevron
/// are decorative and `accessibilityHidden`. A real VoiceOver pass on a device
/// remains open (#2192).
@MainActor
struct ToolActivitySummaryAccessibilityTests {
    private let english = Locale(identifier: "en_US")
    private let base = Date(timeIntervalSince1970: 1_700_000_000)

    private var failedRun: ToolActivityRun {
        ToolActivityRun(tools: [
            tool("entity-search", isError: true, start: base, duration: 2),
            tool("web-fetch", start: base.addingTimeInterval(2), duration: 1),
        ])
    }

    @Test func theCollapsedSummaryCarriesCountSpanAndFailure() {
        let summary = failedRun.summaryAccessibility(expanded: false, locale: english)
        #expect(summary.label == "2 steps, 3 seconds, a step failed")
        #expect(summary.value == "Collapsed")
    }

    @Test func activationFlipsTheValueWithoutChangingTheLabel() {
        let turnID = UUID()
        let disclosure = ToolActivityDisclosure()

        func summary() -> ToolActivityRun.SummaryAccessibility {
            failedRun.summaryAccessibility(
                expanded: disclosure.isExpanded(turnID, default: false),
                locale: english
            )
        }

        let collapsed = summary()
        #expect(collapsed.value == "Collapsed")

        // The same toggle the summary button runs on tap.
        disclosure.toggle(turnID, default: false)
        let expanded = summary()
        #expect(expanded.value == "Expanded")
        #expect(expanded.label == collapsed.label)

        disclosure.toggle(turnID, default: false)
        #expect(summary().value == "Collapsed")
    }

    /// The exposed element is exactly the spoken label and the disclosure value:
    /// there is no field through which a glyph, the chevron or an icon label
    /// could reach VoiceOver. The strip is still drawn, so the silence comes from
    /// hiding it, not from deleting it (#2192).
    @Test func theExposedElementIsOnlyLabelAndValue() {
        let summary = failedRun.summaryAccessibility(expanded: false, locale: english)
        let fields = Mirror(reflecting: summary).children.compactMap(\.label)
        #expect(fields == ["label", "value"])
        #expect(failedRun.linePieces(at: .large).contains(.glyphStrip))
    }
}
