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

    /// The breakdown is the web console's line: the names a reader would
    /// recognise, in order of first use, with a ×N multiplier for repeats.
    @Test func namesUpToThreeToolsWithMultipliers() {
        let run = ToolActivityRun(tools: [
            tool("entity-search"),
            tool("web-fetch"),
            tool("entity-search"),
        ])
        #expect(run.breakdown == "Entity search ×2, Web fetch")
    }

    @Test func summarizesTheTailOfALongBreakdown() {
        let run = ToolActivityRun(tools: [
            tool("entity-search"),
            tool("web-fetch"),
            tool("document-get"),
            tool("schema-list"),
            tool("project-get"),
        ])
        #expect(run.breakdown == "Entity search, Web fetch, Document get, +2 more")
    }

    @Test func noBreakdownWhenNoCallCarriesAName() {
        #expect(ToolActivityRun(tools: [tool("")]).breakdown == nil)
        #expect(ToolActivityRun(tools: []).breakdown == nil)
    }

    /// A wire name is made readable exactly as the web console's
    /// `humanizeToolName` does it, so both surfaces print the same words.
    @Test func humanizesTheWireNameLikeTheWebConsole() {
        #expect(ToolActivityRun.humanized("entity-search") == "Entity search")
        #expect(ToolActivityRun.humanized("search_entities") == "Search entities")
        #expect(ToolActivityRun.humanized("web.search") == "Web search")
        #expect(ToolActivityRun.humanized("  web   fetch  ") == "Web fetch")
        // The web console lowercases the rest of the name before capitalizing
        // the first letter, so both surfaces print the same words.
        #expect(ToolActivityRun.humanized("MCP-Server") == "Mcp server")
    }

    @Test func countsTheFailuresSoACollapsedRunCannotHideThem() {
        let run = ToolActivityRun(tools: [
            tool("entity-search", isError: true),
            tool("web-fetch"),
            tool("document-get", isError: true),
        ])
        #expect(run.failedCount == 2)
        #expect(run.hasError)
        #expect(ToolActivityRun(tools: [tool("entity-search")]).failedCount == 0)
        #expect(!ToolActivityRun(tools: [tool("entity-search")]).hasError)
    }
}

// MARK: - Spoken line

struct ToolActivitySpokenLineTests {
    private let english = Locale(identifier: "en_US")

    @Test func readsTheCountAndTheNames() {
        let run = ToolActivityRun(tools: [
            tool("entity-search"),
            tool("web-fetch"),
        ])
        #expect(run.spokenLine(locale: english) == "2 tool calls, Entity search, Web fetch")
    }

    @Test func readsTheCountWhenNoCallCarriesAName() {
        let run = ToolActivityRun(tools: [tool(""), tool("")])
        #expect(run.spokenLine(locale: english) == "2 tool calls")
    }

    /// One call reads as one call, as it does on the web console.
    @Test func readsASingleCallInTheSingular() {
        let run = ToolActivityRun(tools: [tool("entity-search")])
        #expect(run.spokenLine(locale: english) == "1 tool call, Entity search")
    }

    @Test func saysHowManyStepsFailed() {
        let run = ToolActivityRun(tools: [
            tool("entity-search", isError: true),
            tool("web-fetch"),
        ])
        #expect(run.spokenLine(locale: english) == "2 tool calls, Entity search, Web fetch, 1 failed")
    }

    /// The count and the failure clause resolve in the caller's locale, not the
    /// process locale. The catalog ships English only today, which is exactly
    /// why nothing else would have caught this (#2125).
    @Test func readsTheCountInTheRequestedLocale() {
        let run = ToolActivityRun(tools: [tool(""), tool("")])
        #expect(run.spokenLine(locale: Locale(identifier: "ar_EG")).hasPrefix("٢"))
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

/// Shared with `ToolActivitySummaryLineTests`: one tool call, settled or running.
func tool(
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
