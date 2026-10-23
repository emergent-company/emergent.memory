import Foundation
@testable import Memory
import SwiftUI
import Testing
import UIKit

// MARK: - Grouping

struct ToolActivityGroupingTests {
    @Test func groupsTwoOrMoreSteps() {
        #expect(!ToolActivityGrouping.isGrouped([]))
        #expect(!ToolActivityGrouping.isGrouped([tool("entity-search")]))
        #expect(ToolActivityGrouping.isGrouped([tool("entity-search"), tool("web-fetch")]))
    }

    @Test @MainActor func groupViewBuildsForOneStepAndMany() {
        // One step keeps the plain chip; the view must build either way.
        _ = ToolActivityGroup(tools: [tool("entity-search")], turnID: UUID())
        _ = ToolActivityGroup(
            tools: [tool("entity-search"), tool("web-fetch", running: true)],
            turnID: UUID()
        )
    }
}

// MARK: - Run summary

struct ToolActivityRunTests {
    @Test func reportsTheStepCount() {
        #expect(ToolActivityRun(tools: [tool("entity-search"), tool("web-fetch")]).steps == 2)
    }

    @Test func oneGlyphPerCategoryInOrderOfFirstUse() {
        let run = ToolActivityRun(tools: [
            tool("entity-search"),
            tool("web-fetch"),
            tool("entity-query"),
            tool("document-get"),
        ])
        #expect(run.icons.map(\.category) == [.memory, .web, .documents])
    }

    @Test func capsTheGlyphsAndStillCountsEveryStep() {
        let tools = [
            tool("entity-search"),
            tool("web-fetch"),
            tool("document-get"),
            tool("schema-list"),
            tool("project-get"),
            tool("skill-list"),
        ]
        let run = ToolActivityRun(tools: tools)
        #expect(run.icons.count == ToolActivityRun.maxIcons)
        #expect(run.icons.map(\.category) == [.memory, .web, .documents, .schemas])
        #expect(run.steps == tools.count)
    }

    @Test func reportsTheStrongestStateInTheCategory() {
        // Error outranks running, running outranks settled.
        let run = ToolActivityRun(tools: [
            tool("entity-search", running: true),
            tool("entity-query", isError: true),
            tool("entity-create"),
            tool("web-fetch", running: true),
            tool("document-get"),
        ])
        let states = Dictionary(uniqueKeysWithValues: run.icons.map { ($0.category, $0.state) })
        #expect(states[.memory] == .failed)
        #expect(states[.web] == .running)
        #expect(states[.documents] == .idle)
    }

    @Test func reportsAFailureSoACollapsedRunCannotHideIt() {
        #expect(!ToolActivityRun(tools: [tool("entity-search")]).hasError)
        #expect(ToolActivityRun(tools: [tool("entity-search"), tool("web-fetch", isError: true)]).hasError)
    }
}

// MARK: - Spoken line

struct ToolActivitySpokenLineTests {
    private let english = Locale(identifier: "en_US")

    @Test func readsTheCountAndTheSpokenTime() {
        let start = Date(timeIntervalSince1970: 1_700_000_000)
        let run = ToolActivityRun(tools: [
            tool("entity-search", start: start, duration: 6),
            tool("web-fetch", start: start.addingTimeInterval(6), duration: 6),
        ])
        #expect(run.spokenLine(locale: english) == "2 steps, 12 seconds")
    }

    @Test func readsTheCountWhenTheRunHasNoTime() {
        let start = Date(timeIntervalSince1970: 1_700_000_000)
        // Still running: no span, so nothing to say about time.
        let running = ToolActivityRun(tools: [
            tool("entity-search", start: start, duration: 6),
            tool("web-fetch", running: true, start: start.addingTimeInterval(6)),
        ])
        #expect(running.spokenLine(locale: english) == "2 steps")

        // Settled but under a second: same rule as a step row, which shows no
        // time below one second either.
        let brief = ToolActivityRun(tools: [
            tool("entity-search", start: start, duration: 0.2),
            tool("web-fetch", start: start.addingTimeInterval(0.2), duration: 0.2),
        ])
        #expect(brief.spokenLine(locale: english) == "2 steps")
    }

    @Test func saysWhenAStepFailed() {
        let start = Date(timeIntervalSince1970: 1_700_000_000)
        let run = ToolActivityRun(tools: [
            tool("entity-search", isError: true, start: start, duration: 2),
            tool("web-fetch", start: start.addingTimeInterval(2), duration: 1),
        ])
        #expect(run.spokenLine(locale: english) == "2 steps, 3 seconds, a step failed")
    }
}

// MARK: - Run span

struct ToolActivitySpanTests {
    @Test func spansFirstStartToLastEnd() {
        let start = Date(timeIntervalSince1970: 1_700_000_000)
        let tools = [
            tool("entity-search", start: start, duration: 4),
            tool("web-fetch", start: start.addingTimeInterval(10), duration: 2),
        ]
        #expect(ToolActivityTiming.span(of: tools) == 12)
    }

    @Test func measuresTheOverlapNotTheSum() {
        let start = Date(timeIntervalSince1970: 1_700_000_000)
        let tools = [
            tool("entity-search", start: start, duration: 6),
            tool("web-fetch", start: start.addingTimeInterval(1), duration: 1),
        ]
        // Ends at 6, not 7: the run occupied six seconds, not seven.
        #expect(ToolActivityTiming.span(of: tools) == 6)
    }

    @Test func noSpanWhileAStepIsStillRunning() {
        let start = Date(timeIntervalSince1970: 1_700_000_000)
        let tools = [
            tool("entity-search", start: start, duration: 4),
            tool("web-fetch", running: true, start: start.addingTimeInterval(5)),
        ]
        #expect(ToolActivityTiming.span(of: tools) == nil)
    }

    @Test func noSpanWithoutTiming() {
        #expect(ToolActivityTiming.span(of: []) == nil)
        #expect(ToolActivityTiming.span(of: [tool("entity-search"), tool("web-fetch")]) == nil)
    }
}

// MARK: - Disclosure

@MainActor
struct ToolActivityDisclosureTests {
    @Test func followsTheDefaultUntilTheReaderTouchesIt() {
        let disclosure = ToolActivityDisclosure()
        let turn = UUID()
        // A running run is open; the same run settled is closed.
        #expect(disclosure.isExpanded(turn, default: true))
        #expect(!disclosure.isExpanded(turn, default: false))
    }

    @Test func keepsTheReadersChoiceWhenTheRunSettles() {
        let disclosure = ToolActivityDisclosure()
        let turn = UUID()
        // Opened by hand while steps were still running.
        disclosure.setExpanded(true, for: turn)
        // The last step settled: the default flips to closed.
        #expect(disclosure.isExpanded(turn, default: false))

        // And the other way: closed by hand, the run does not reopen itself.
        disclosure.setExpanded(false, for: turn)
        #expect(!disclosure.isExpanded(turn, default: true))
    }

    @Test func keysTheChoiceByTurn() {
        let disclosure = ToolActivityDisclosure()
        let first = UUID()
        let second = UUID()
        disclosure.setExpanded(true, for: first)
        #expect(disclosure.isExpanded(first, default: false))
        #expect(!disclosure.isExpanded(second, default: false))
    }

    @Test func clearForgetsEveryChoice() {
        let disclosure = ToolActivityDisclosure()
        let turn = UUID()
        disclosure.setExpanded(true, for: turn)
        disclosure.clear()
        #expect(!disclosure.isExpanded(turn, default: false))
    }
}

// MARK: - Fixtures

private func tool(
    _ name: String,
    running: Bool = false,
    isError: Bool = false,
    start: Date? = nil,
    duration: TimeInterval? = nil
) -> ToolCallInfo {
    ToolCallInfo(
        id: name,
        name: name,
        arguments: #"{"query":"retrieval"}"#,
        result: running ? nil : "ok",
        isError: isError,
        isRunning: running,
        startedAt: start,
        duration: duration
    )
}

// MARK: - Layout

/// The summary line is the only route to a closed run's rows, so its footprint
/// and the reader's control over the rows are measured, not assumed. The app has
/// no UI-test target: a real layout pass is the closest thing to the device.
@MainActor
struct ToolActivityGroupLayoutTests {
    private let width: CGFloat = 390
    private let base = Date(timeIntervalSince1970: 1_700_000_000)

    private var tools: [ToolCallInfo] {
        [
            tool("entity-search", start: base, duration: 6),
            tool("web-fetch", start: base.addingTimeInterval(6), duration: 6),
        ]
    }

    @Test func aSettledRunIsOneTapTarget() {
        let disclosure = ToolActivityDisclosure()
        let height = height(of: group(disclosure))
        // The 44pt minimum, not a line of 13pt text.
        #expect(height >= 44)
        #expect(height < 56)
    }

    @Test func theReadersChoiceRevealsTheRows() {
        let turn = UUID()
        let closed = height(of: group(ToolActivityDisclosure(), turnID: turn))

        let opened = ToolActivityDisclosure()
        opened.setExpanded(true, for: turn)
        let expanded = height(of: group(opened, turnID: turn))

        // Two step chips, not just the line they are hidden behind.
        #expect(expanded > closed + 30)
    }

    @Test func aSingleStepIsJustItsRow() {
        let disclosure = ToolActivityDisclosure()
        let one = ToolActivityGroup(tools: [tools[0]], turnID: UUID())
            .environment(\.toolActivityDisclosure, disclosure)
        // No summary line above it: the row alone, still a full tap target.
        let height = height(of: one)
        #expect(height >= 44)
        #expect(height < 90)
    }

    private func group(_ disclosure: ToolActivityDisclosure, turnID: UUID = UUID()) -> some View {
        ToolActivityGroup(tools: tools, turnID: turnID)
            .environment(\.toolActivityDisclosure, disclosure)
    }

    /// Runs a real SwiftUI layout pass at phone width with an unbounded height
    /// proposal, so frames, fonts and the disclosure all take effect.
    private func height(of view: some View) -> CGFloat {
        let host = UIHostingController(rootView: view)
        _ = host.view
        let fitted = host.sizeThatFits(in: CGSize(width: width, height: .greatestFiniteMagnitude))
        #expect(
            fitted.height > 0 && fitted.height < 10000,
            Comment(rawValue: "unexpected laid-out height \(fitted.height) at width \(width)")
        )
        return fitted.height
    }
}
