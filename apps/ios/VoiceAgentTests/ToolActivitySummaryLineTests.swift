import Foundation
@testable import Memory
import SwiftUI
import Testing
import UIKit

// MARK: - Line contents

/// The view draws `linePieces` one-to-one, so the rule that decides what the
/// summary line says lives on the model, where it can be asserted — including
/// the span, which a height measurement cannot see on a line that fits (#2125).
struct ToolActivityLinePieceTests {
    private let base = Date(timeIntervalSince1970: 1_700_000_000)

    private var timed: ToolActivityRun {
        ToolActivityRun(tools: [
            tool("entity-search", start: base, duration: 6),
            tool("web-fetch", start: base.addingTimeInterval(6), duration: 6),
        ])
    }

    @Test func drawsStripCountAndSpanAtDefaultSizes() {
        #expect(timed.linePieces(at: .large) == [.glyphStrip, .steps, .span])
    }

    @Test func dropsTheDecorativeStripAtAccessibilitySizes() {
        // The strip is the only piece that goes: the count, the span and a
        // failure still have to be said.
        #expect(timed.linePieces(at: .accessibility1) == [.steps, .span])
        #expect(timed.linePieces(at: .accessibility5) == [.steps, .span])
    }

    @Test func alwaysDrawsTheCount() {
        // No timing and no failure: the count is all a closed run has left.
        let untimed = ToolActivityRun(tools: [tool("entity-search"), tool("web-fetch")])
        #expect(untimed.linePieces(at: .large) == [.glyphStrip, .steps])
        #expect(untimed.linePieces(at: .accessibility5) == [.steps])
    }

    @Test func drawsAFailureAfterTheSpan() {
        let failed = failedRun
        #expect(failed.linePieces(at: .large) == [.glyphStrip, .steps, .span, .failure])
    }

    /// The doc comment on `linePieces` claims the failure marker survives
    /// accessibility sizes along with the count and the span; this pins every
    /// supported size, not just `.large` (#2192).
    @Test func drawsTheFailureAtEveryTypeSize() {
        let failed = failedRun
        for size in DynamicTypeSize.allCases {
            let expected: [ToolActivityRun.LinePiece] = size.isAccessibilitySize
                ? [.steps, .span, .failure]
                : [.glyphStrip, .steps, .span, .failure]
            #expect(
                failed.linePieces(at: size) == expected,
                Comment(rawValue: "unexpected pieces at \(size)")
            )
        }
    }

    /// The span decision is numeric and locale-independent, and the formatter
    /// uses the same predicate: no locale can make `linePieces` claim a span the
    /// view then declines to draw (#2192).
    @Test func decidesTheSpanIndependentlyOfLocale() {
        #expect(!ToolActivityTiming.isReadable(nil))
        #expect(!ToolActivityTiming.isReadable(0))
        #expect(!ToolActivityTiming.isReadable(0.999))
        #expect(!ToolActivityTiming.isReadable(.infinity))
        #expect(!ToolActivityTiming.isReadable(.nan))
        #expect(ToolActivityTiming.isReadable(1))
        #expect(ToolActivityTiming.isReadable(12))

        let locales = [
            Locale(identifier: "en_US"),
            Locale(identifier: "de_DE"),
            Locale(identifier: "ar_EG"),
            Locale(identifier: "ja_JP"),
        ]
        for duration in [0.2, 0.999, 1, 12] {
            let run = ToolActivityRun(tools: [
                tool("entity-search", start: base, duration: duration),
                tool("web-fetch", start: base.addingTimeInterval(duration), duration: duration),
            ])
            for locale in locales {
                let formats = ToolActivityTiming.text(run.span, locale: locale) != nil
                #expect(
                    formats == ToolActivityTiming.isReadable(run.span),
                    Comment(rawValue: "formatter and predicate disagree in \(locale) for \(duration)s")
                )
                #expect(
                    run.linePieces(at: .large).contains(.span) == formats,
                    Comment(rawValue: "pieces and formatter disagree in \(locale) for \(duration)s")
                )
            }
        }
    }

    private var failedRun: ToolActivityRun {
        ToolActivityRun(tools: [
            tool("entity-search", isError: true, start: base, duration: 2),
            tool("web-fetch", start: base.addingTimeInterval(2), duration: 1),
        ])
    }

    @Test func leavesOutASpanTooShortToRead() {
        // Same rule as a step row: under a second there is nothing to show.
        let brief = ToolActivityRun(tools: [
            tool("entity-search", start: base, duration: 0.2),
            tool("web-fetch", start: base.addingTimeInterval(0.2), duration: 0.2),
        ])
        #expect(brief.linePieces(at: .large) == [.glyphStrip, .steps])
    }
}

// MARK: - Layout

/// The summary line is the only route to a closed run's rows, so at
/// accessibility text sizes its footprint is measured, not assumed: the app has
/// no UI-test target, and a real layout pass is the closest thing to the device.
@MainActor
struct ToolActivitySummaryLineLayoutTests {
    /// A modern phone width, where the summary line has to stay one line.
    private let phoneWidth: CGFloat = 390
    private let base = Date(timeIntervalSince1970: 1_700_000_000)

    /// Five steps across four categories: the widest summary line the run can
    /// draw — four glyphs, the count, a span and the chevron.
    private var crowded: [ToolCallInfo] {
        [
            tool("entity-search", start: base, duration: 6),
            tool("web-fetch", start: base.addingTimeInterval(6), duration: 6),
            tool("entity-query", start: base.addingTimeInterval(12), duration: 3),
            tool("document-get", start: base.addingTimeInterval(15), duration: 2),
            tool("schema-list", start: base.addingTimeInterval(17), duration: 1),
        ]
    }

    private var summaryLine: some View {
        ToolActivityGroup(tools: crowded, turnID: UUID())
            .environment(\.toolActivityDisclosure, ToolActivityDisclosure())
    }

    /// The count is the only summary a closed run has left, so at accessibility
    /// sizes it must not be the thing that gives way: the decorative glyph strip
    /// comes off the line and the count — with the room the strip used — stays a
    /// single line at phone width, span and chevron included. The #2124 line
    /// measured ~1.7 count-lines here, because the strip kept its four scaled
    /// glyphs and left the count and the span to wrap around each other.
    @Test func theCountKeepsThePhoneWidthLineToItselfAtAccessibilityTextSizes() {
        let oneLine = oneLineCountHeight(at: .accessibility5)
        let atAccessibility5 = height(of: summaryLine, at: .accessibility5)

        #expect(
            atAccessibility5 <= oneLine * 1.2,
            Comment(rawValue: "summary line measured \(atAccessibility5)pt at .accessibility5; one line of the count font is \(oneLine)pt")
        )
    }

    /// When the line cannot hold the count beside the span and the chevron, the
    /// count wraps instead of ellipsising: a `lineLimit(1)` count stays exactly
    /// one line tall however narrow the proposal gets.
    ///
    /// 280pt is narrower than any phone at an accessibility size and wider than
    /// the count's longest word, so the count wraps at its space. Below ~265pt
    /// not even one word fits and the count breaks mid-word — no device width
    /// asks for that — and from ~310pt up the count already fits on one line.
    /// The upper bound keeps the opposite failure away: pieces squeezed so hard
    /// that they wrap a character per line.
    @Test func theCountWrapsInsteadOfTruncatingOnANarrowLineAtAccessibilityTextSizes() {
        let oneLine = oneLineCountHeight(at: .accessibility5)
        let narrow = height(of: summaryLine, at: .accessibility5, width: 280)

        #expect(
            narrow >= oneLine * 1.5,
            Comment(rawValue: "narrow line \(narrow)pt; one line of the count font is \(oneLine)pt")
        )
        #expect(
            narrow <= oneLine * 2.5,
            Comment(rawValue: "narrow line \(narrow)pt; one line of the count font is \(oneLine)pt")
        )
    }

    /// Runs a real SwiftUI layout pass with an unbounded height proposal at
    /// `size`, so frames, fonts and the disclosure all take effect.
    private func height(of view: some View, at size: DynamicTypeSize, width: CGFloat? = nil) -> CGFloat {
        let measuredWidth = width ?? phoneWidth
        let host = UIHostingController(rootView: view.environment(\.dynamicTypeSize, size))
        _ = host.view
        let fitted = host.sizeThatFits(in: CGSize(width: measuredWidth, height: .greatestFiniteMagnitude))
        #expect(
            fitted.height > 0 && fitted.height < 10000,
            Comment(rawValue: "unexpected laid-out height \(fitted.height) at width \(measuredWidth)")
        )
        return fitted.height
    }

    /// One line of the count's font at `size`, measured at its ideal size so a
    /// wrapping proposal cannot shrink it.
    private func oneLineCountHeight(at size: DynamicTypeSize) -> CGFloat {
        let host = UIHostingController(
            rootView: Text(verbatim: "5 steps")
                .font(MemoryFont.bodySmall)
                .lineLimit(1)
                .fixedSize()
                .environment(\.dynamicTypeSize, size)
        )
        _ = host.view
        return host.sizeThatFits(in: CGSize(width: phoneWidth, height: .greatestFiniteMagnitude)).height
    }
}
