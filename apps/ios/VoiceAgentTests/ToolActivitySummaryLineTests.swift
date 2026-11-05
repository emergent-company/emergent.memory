import Foundation
@testable import Memory
import SwiftUI
import Testing
import UIKit

// MARK: - Line contents

/// The view draws `linePieces` one-to-one, so the rule that decides what the
/// summary line says lives on the model, where it can be asserted.
///
/// The pieces are the web console's, in the same order: the call count, the
/// names of the tools involved, and how many steps failed. At accessibility
/// text sizes the names give way so the count and the failure stay whole —
/// the same reason the old summary dropped its decorative glyph strip there.
struct ToolActivityLinePieceTests {
    private let base = Date(timeIntervalSince1970: 1_700_000_000)

    private var timed: ToolActivityRun {
        ToolActivityRun(tools: [
            tool("entity-search", start: base, duration: 6),
            tool("web-fetch", start: base.addingTimeInterval(6), duration: 6),
        ])
    }

    @Test func drawsTheCountAndTheNames() {
        #expect(timed.linePieces(at: .large) == [.calls, .breakdown])
    }

    @Test func drawsTheFailureAfterTheNames() {
        let failed = failedRun
        #expect(failed.linePieces(at: .large) == [.calls, .breakdown, .failed])
    }

    @Test func alwaysDrawsTheCount() {
        // No names, no failure: the count is all a closed run has left.
        let unnamed = ToolActivityRun(tools: [tool(""), tool("")])
        #expect(unnamed.linePieces(at: .large) == [.calls])
        #expect(unnamed.linePieces(at: .accessibility5) == [.calls])
    }

    /// At accessibility sizes the names are the piece that gives way: the count
    /// and the failure are what a closed run has to keep legible (#2192).
    @Test func dropsTheNamesAtAccessibilitySizes() {
        for size in DynamicTypeSize.allCases {
            let expected: [ToolActivityRun.LinePiece] = size.isAccessibilitySize
                ? [.calls, .failed]
                : [.calls, .breakdown, .failed]
            #expect(
                failedRun.linePieces(at: size) == expected,
                Comment(rawValue: "unexpected pieces at \(size)")
            )
        }
    }

    @Test func anUnnamedRunStillReadsAsToolCalls() {
        let unnamed = ToolActivityRun(tools: [tool(""), tool("")])
        #expect(unnamed.spokenLine(locale: Locale(identifier: "en_US")) == "2 tool calls")
    }

    private var failedRun: ToolActivityRun {
        ToolActivityRun(tools: [
            tool("entity-search", isError: true, start: base, duration: 2),
            tool("web-fetch", start: base.addingTimeInterval(2), duration: 1),
        ])
    }
}

// MARK: - Timing

/// The expanded step's duration is worded exactly as the web console words it,
/// so the same call opened on either surface prints the same string.
struct ToolActivityMetaTextTests {
    @Test func wordsDurationsLikeTheWebConsole() {
        #expect(ToolActivityTiming.metaText(nil) == nil)
        #expect(ToolActivityTiming.metaText(0) == "0ms")
        #expect(ToolActivityTiming.metaText(0.85) == "850ms")
        #expect(ToolActivityTiming.metaText(0.999) == "999ms")
        #expect(ToolActivityTiming.metaText(1) == "1s")
        #expect(ToolActivityTiming.metaText(1.2) == "1.2s")
        #expect(ToolActivityTiming.metaText(5) == "5s")
        #expect(ToolActivityTiming.metaText(60) == "1m")
        #expect(ToolActivityTiming.metaText(125) == "2m")
        #expect(ToolActivityTiming.metaText(3600) == "1h 0m")
        #expect(ToolActivityTiming.metaText(8100) == "2h 15m")
    }

    @Test func refusesUnusableDurations() {
        #expect(ToolActivityTiming.metaText(-1) == nil)
        #expect(ToolActivityTiming.metaText(.infinity) == nil)
        #expect(ToolActivityTiming.metaText(.nan) == nil)
    }

    /// The readability decision stays numeric and locale-independent, and the
    /// formatter uses the same predicate: no locale can make the working line's
    /// text disagree with the predicate that gates it (#2192).
    @Test func decidesReadabilityIndependentlyOfLocale() {
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
            for locale in locales {
                let formats = ToolActivityTiming.text(duration, locale: locale) != nil
                #expect(
                    formats == ToolActivityTiming.isReadable(duration),
                    Comment(rawValue: "formatter and predicate disagree in \(locale) for \(duration)s")
                )
            }
        }
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
    /// draw — three names, a "+N more", a failure count and the chevron.
    private var crowded: [ToolCallInfo] {
        [
            tool("entity-search", start: base, duration: 6),
            tool("web-fetch", start: base.addingTimeInterval(6), duration: 6),
            tool("entity-query", start: base.addingTimeInterval(12), duration: 3),
            tool("document-get", start: base.addingTimeInterval(15), duration: 2),
            tool("schema-list", isError: true, start: base.addingTimeInterval(17), duration: 1),
        ]
    }

    private var summaryLine: some View {
        ToolActivityGroup(tools: crowded, turnID: UUID())
            .environment(\.toolActivityDisclosure, ToolActivityDisclosure())
    }

    /// Every piece is single-line, so the summary keeps its 44pt tap target at
    /// every text size instead of growing into a paragraph above a folded run.
    @Test func theSummaryIsOneLineAtAccessibilityTextSizes() {
        for size in DynamicTypeSize.allCases {
            let oneLine = oneLineCountHeight(at: size)
            let height = height(of: summaryLine, at: size)
            #expect(
                height <= max(44, oneLine * 1.6),
                Comment(rawValue: "summary line measured \(height)pt at \(size); one line of the count font is \(oneLine)pt")
            )
        }
    }

    /// The names are the piece that gives way: at a width no phone offers they
    /// truncate, and the line still measures one line tall rather than wrapping.
    @Test func theNamesTruncateInsteadOfWrapping() {
        let oneLine = oneLineCountHeight(at: .accessibility5)
        let narrow = height(of: summaryLine, at: .accessibility5, width: 280)
        #expect(
            narrow <= max(44, oneLine * 1.6),
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
            rootView: Text(verbatim: "5 tool calls")
                .font(MemoryFont.bodySmall)
                .lineLimit(1)
                .fixedSize()
                .environment(\.dynamicTypeSize, size)
        )
        _ = host.view
        return host.sizeThatFits(in: CGSize(width: phoneWidth, height: .greatestFiniteMagnitude)).height
    }
}
