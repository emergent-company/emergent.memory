import Foundation
@testable import Memory
import Testing

/// The turn footer's placement: the last reply of a run that has an end
/// boundary, and only that. A run still streaming, or one whose record carries
/// no end, must show nothing; the boundary rows themselves never draw.
@MainActor
struct TurnFooterPlacementTests {
    private let gmt = TimeZone.gmt
    private let british = Locale(identifier: "en_GB")

    private func date(_ string: String) -> Date {
        ISO8601DateFormatter().date(from: string)!
    }

    private func reply(_ id: String, runID: String?, _ text: String = "answer") -> TimelineItem {
        TimelineItem(
            id: id,
            runID: runID,
            createdAt: nil,
            kind: .assistantText(markdown: text, citations: [])
        )
    }

    private func boundary(
        id: String,
        runID: String,
        model: String? = nil,
        status: String? = "completed",
        start: Date?,
        completed: Date? = nil
    ) -> TimelineItem {
        TimelineItem(
            id: id,
            runID: runID,
            createdAt: start,
            kind: .runBoundary(RunWindow(
                id: runID,
                model: model,
                status: status,
                startedAt: start,
                completedAt: completed
            ))
        )
    }

    @Test func theLastReplyOfAnEndedRunCarriesItsWindow() throws {
        let start = date("2026-11-08T10:00:00Z")
        let end = date("2026-11-08T10:02:03Z")
        let items = [
            boundary(id: "start-r1", runID: "r1", model: "test-model", start: start),
            reply("a1", runID: "r1", "first"),
            reply("a2", runID: "r1", "last"),
            boundary(id: "end-r1", runID: "r1", start: start, completed: end),
        ]

        let footers = TurnFooterPlacement.byAssistant(from: items)

        // One footer per turn: only the run's last reply carries it.
        #expect(footers.count == 1)
        #expect(footers["a1"] == nil)
        #expect(footers["a2"] == TurnRunInfo(model: "test-model", startedAt: start, endedAt: end))
        let line = try TurnFooterModel(
            run: #require(footers["a2"]),
            timeZone: gmt,
            locale: british
        ).line
        #expect(line == "test-model · 2:03 · 10:02")
    }

    @Test func aRunStillGoingHasNoFooter() {
        let start = date("2026-11-08T10:00:00Z")
        let items = [
            boundary(id: "start-r1", runID: "r1", model: "test-model", start: start),
            reply("a1", runID: "r1"),
        ]

        #expect(TurnFooterPlacement.byAssistant(from: items).isEmpty)
    }

    /// A recorded `run_end` without `completed_at` cannot be dated, so it must
    /// not produce a footer (and never a "0:00" one).
    @Test func anEndBoundaryWithoutAnEndTimeHasNoFooter() {
        let start = date("2026-11-08T10:00:00Z")
        let items = [
            boundary(id: "start-r1", runID: "r1", model: "test-model", start: start),
            reply("a1", runID: "r1"),
            boundary(id: "end-r1", runID: "r1", start: start),
        ]

        #expect(TurnFooterPlacement.byAssistant(from: items).isEmpty)
    }

    @Test func eachEndedRunGetsItsOwnFooterOnItsOwnReply() {
        let firstStart = date("2026-11-08T10:00:00Z")
        let firstEnd = date("2026-11-08T10:00:07Z")
        let secondStart = date("2026-11-08T10:05:00Z")
        let secondEnd = date("2026-11-08T10:05:30Z")
        let items = [
            boundary(id: "start-r1", runID: "r1", model: "one", start: firstStart),
            reply("a1", runID: "r1"),
            boundary(id: "end-r1", runID: "r1", start: firstStart, completed: firstEnd),
            boundary(id: "start-r2", runID: "r2", model: "two", start: secondStart),
            reply("a2", runID: "r2"),
            boundary(id: "end-r2", runID: "r2", start: secondStart, completed: secondEnd),
        ]

        let footers = TurnFooterPlacement.byAssistant(from: items)

        #expect(footers["a1"]?.model == "one")
        #expect(footers["a2"]?.model == "two")
        #expect(footers.count == 2)
    }

    /// A live window: the stream sends no model, so the line carries the
    /// duration and end clock until the recorded history supplies one.
    @Test func aLiveWindowDrawsWithoutAModel() throws {
        let start = date("2026-11-08T09:59:53Z")
        let end = date("2026-11-08T10:00:00Z")
        let items = [
            reply("a1", runID: "r9"),
            boundary(id: "live-run-end-r9", runID: "r9", start: start, completed: end),
        ]

        let footers = TurnFooterPlacement.byAssistant(from: items)

        #expect(footers["a1"] == TurnRunInfo(model: nil, startedAt: start, endedAt: end))
        let line = try TurnFooterModel(
            run: #require(footers["a1"]),
            timeZone: gmt,
            locale: british
        ).line
        #expect(line == "0:07 · 10:00")
    }

    /// The live stream's terminal frame is the run's end boundary
    /// (`ChatTimelineLive`): before it there is no footer, after it there is
    /// one, and a replayed terminal frame does not stack a second window.
    @Test func theLiveStoreOpensItsWindowAtMetaAndClosesItAtDone() {
        var now = date("2026-11-08T10:00:00Z")
        let store = ChatTimelineStore(now: { now })
        store.apply(.meta(conversationID: "c1", runID: "r1"))
        store.apply(.text("the answer"))

        #expect(TurnFooterPlacement.byAssistant(from: store.items).isEmpty)

        now = date("2026-11-08T10:00:07Z")
        store.apply(.done)

        let footers = TurnFooterPlacement.byAssistant(from: store.items)
        #expect(footers["live-assistant-1"] == TurnRunInfo(
            model: nil,
            startedAt: date("2026-11-08T10:00:00Z"),
            endedAt: now
        ))

        store.apply(.done)
        let windows = store.items.filter { item in
            if case .runBoundary = item.kind {
                return true
            }
            return false
        }
        #expect(windows.count == 1)
    }

    /// "Copy turn" writes that reply's plain text, through the same
    /// injectable seam and confirmation window as every other copy control.
    @Test func copyTurnYieldsTheReplyText() async {
        var written: [String] = []
        let feedback = CopyFeedback(sleep: { _ in })

        await feedback.copy(CopyIntent.turn("## Result\n\n**3** items.").text, write: {
            written.append($0)
        }).value

        #expect(written == ["Result\n\n3 items."])
        #expect(feedback.duration == CopyFeedback.confirmationWindow)
    }
}
