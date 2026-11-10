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
        _ = ToolActivityGroup(tools: [tool("entity-search")], groupID: "group")
        _ = ToolActivityGroup(
            tools: [tool("entity-search"), tool("web-fetch", running: true)],
            groupID: "group"
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
struct ChatDisclosureTests {
    @Test func followsTheDefaultUntilTheReaderTouchesIt() {
        let disclosure = ChatDisclosure()
        let group = "call-1"
        // A running run is open; the same run settled is closed.
        #expect(disclosure.isExpanded(group, default: true))
        #expect(!disclosure.isExpanded(group, default: false))
    }

    /// A reader who opens a running group keeps it open when it settles: the
    /// default flips from open to closed, the recorded choice does not.
    @Test func keepsTheReadersOpenChoiceWhenTheRunSettles() {
        let disclosure = ChatDisclosure()
        let group = "call-1"
        // Opened by hand while steps were still running.
        #expect(disclosure.isExpanded(group, default: true))
        disclosure.setExpanded(true, for: group)
        // The last step settled: the default flips to closed.
        #expect(disclosure.isExpanded(group, default: false))

        // And the other way: closed by hand, the run does not reopen itself.
        disclosure.setExpanded(false, for: group)
        #expect(!disclosure.isExpanded(group, default: true))
    }

    /// Two runs in one turn are independent: tapping the first must not change
    /// the second. This is the regression the per-turn key caused.
    @Test func twoGroupsInOneTurnOpenIndependently() {
        let disclosure = ChatDisclosure()
        var turn = ChatTurn(anchorMessageID: "message-1")
        turn.toolGroups = [
            ToolCallGroup(id: "call-1", tools: []),
            ToolCallGroup(id: "call-3", tools: []),
        ]

        disclosure.setExpanded(true, for: turn.toolGroups[0].id)
        #expect(disclosure.isExpanded(turn.toolGroups[0].id, default: false))
        // The second run keeps the settled default: closed.
        #expect(!disclosure.isExpanded(turn.toolGroups[1].id, default: false))
    }

    /// A thinking block remembers its own choice too, and the choice survives
    /// the block being replaced when the turn settles.
    @Test func thinkingStateIsRememberedPerBlockAndSurvivesSettling() {
        let disclosure = ChatDisclosure()
        var turn = ChatTurn(anchorMessageID: "message-1")
        turn.thinkingBlocks = [
            ThinkingBlock(id: "thinking-1", text: "reasoning", isLive: true, isOpen: true),
            ThinkingBlock(id: "thinking-2", text: "more", isLive: true, isOpen: true),
        ]
        let blockID = turn.thinkingBlocks[0].id
        #expect(!disclosure.isThinkingExpanded(blockID))

        disclosure.toggleThinking(blockID)
        #expect(disclosure.isThinkingExpanded(blockID))
        #expect(!disclosure.isThinkingExpanded(turn.thinkingBlocks[1].id))

        // The turn settles: the same block id is rendered with isLive == false.
        turn.thinkingBlocks[0] = ThinkingBlock(id: blockID, text: "reasoning done", isLive: false, isOpen: false)
        #expect(disclosure.isThinkingExpanded(turn.thinkingBlocks[0].id))
    }

    @Test func clearForgetsEveryChoice() {
        let disclosure = ChatDisclosure()
        disclosure.setExpanded(true, for: "call-1")
        disclosure.setThinkingExpanded(true, for: "thinking-1")
        disclosure.setSourcesExpanded(true, for: "message-1")
        disclosure.clear()
        #expect(!disclosure.isExpanded("call-1", default: false))
        #expect(!disclosure.isThinkingExpanded("thinking-1"))
        #expect(!disclosure.isSourcesExpanded("message-1"))
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
        let disclosure = ChatDisclosure()
        let height = height(of: group(disclosure))
        // The 44pt minimum, not a line of 13pt text.
        #expect(height >= 44)
        #expect(height < 56)
    }

    @Test func theReadersChoiceRevealsTheRows() {
        let groupID = "call-1"
        let closed = height(of: group(ChatDisclosure(), groupID: groupID))

        let opened = ChatDisclosure()
        opened.setExpanded(true, for: groupID)
        let expanded = height(of: group(opened, groupID: groupID))

        // Two step chips, not just the line they are hidden behind.
        #expect(expanded > closed + 30)
    }

    @Test func aSingleStepIsJustItsRow() {
        let disclosure = ChatDisclosure()
        let one = ToolActivityGroup(tools: [tools[0]], groupID: "group")
            .environment(\.chatDisclosure, disclosure)
        // No summary line above it: the row alone, still a full tap target.
        let height = height(of: one)
        #expect(height >= 44)
        #expect(height < 90)
    }

    /// Two runs in one turn open independently: opening the first must not
    /// reveal the second's rows. The per-turn key used to expand both at once.
    @Test func openingOneGroupLeavesTheOtherFolded() {
        let disclosure = ChatDisclosure()
        let bothFolded = height(of: twoGroups(disclosure))

        disclosure.setExpanded(true, for: "call-a")
        let oneOpen = height(of: twoGroups(disclosure))
        #expect(oneOpen > bothFolded + 30)

        disclosure.setExpanded(true, for: "call-b")
        let bothOpen = height(of: twoGroups(disclosure))
        #expect(bothOpen > oneOpen + 30)
    }

    private func twoGroups(_ disclosure: ChatDisclosure) -> some View {
        VStack(alignment: .leading) {
            ToolActivityGroup(tools: tools, groupID: "call-a")
            ToolActivityGroup(tools: tools, groupID: "call-b")
        }
        .environment(\.chatDisclosure, disclosure)
    }

    private func group(_ disclosure: ChatDisclosure, groupID: String = "group") -> some View {
        ToolActivityGroup(tools: tools, groupID: groupID)
            .environment(\.chatDisclosure, disclosure)
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
