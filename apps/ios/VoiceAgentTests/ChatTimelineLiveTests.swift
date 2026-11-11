import Foundation
@testable import Memory
import Testing

/// The live half of the ordered timeline: the same turn streamed as frames must
/// draw the same rows as the record, a reload must keep the reader's disclosure
/// state, and one loader call must feed both the record baseline and the rows
/// (`emergent.memory#435`).
///
/// Written and reviewed but not run locally: there is no Swift toolchain on this
/// host (see the PR body).
@MainActor
struct ChatTimelineLiveTests {
    // MARK: - Live/history parity

    /// The same turn streamed live must produce the same drawn kinds, in the
    /// same order, with the same text — this is the bug the shared timeline
    /// fixes (`ChatTurnActivityView` drew think · tools · all text).
    @Test func liveFramesRenderTheSameOrderAsHistory() throws {
        let store = ChatTimelineStore()
        store.apply(.meta(conversationID: "conv-1", runID: "r1"))
        store.appendUserMessage(id: "u-live", text: "hi")
        store.apply(.activity(.thinking(id: "live-think", delta: "thinking one")))
        store.apply(.activity(.thinkingDone(id: "live-think")))
        store.apply(.activity(.toolCall(ChatToolCallEvent(id: "t1", tool: "entity_search", arguments: "{}"))))
        store.apply(.activity(.toolResult(ChatToolResultEvent(id: "t1", tool: "entity_search", result: "{}", isError: false))))
        store.apply(.text("Let me check…"))
        store.apply(.activity(.toolCall(ChatToolCallEvent(id: "t2", tool: "entity_search", arguments: "{}"))))
        store.apply(.activity(.toolResult(ChatToolResultEvent(id: "t2", tool: "entity_search", result: "{}", isError: false))))
        store.apply(.text("Here is the answer."))
        store.apply(.done)

        let recorded = try Fixture.recorded()
        let fromRecord = Fixture.drawn(recorded)
        let live = store.items

        // `.done` now appends the run's end boundary (the turn footer's window),
        // which draws no row — compare what the chat draws, not the raw store.
        #expect(Fixture.labels(Fixture.drawn(live)) == Fixture.labels(fromRecord))
        #expect(Fixture.assistantTexts(live) == Fixture.assistantTexts(fromRecord))
        #expect(Fixture.reasoningTexts(live) == Fixture.reasoningTexts(fromRecord))
        #expect(Fixture.toolIDs(live) == Fixture.toolIDs(fromRecord))
    }

    // MARK: - Row identity (the disclosure key)

    @Test func consecutiveToolsFoldIntoOneGroupKeyedByTheFirstCall() {
        let items = [
            TimelineItem(id: "t1", runID: "r1", createdAt: nil, kind: .tool(Fixture.tool("t1"))),
            TimelineItem(id: "t2", runID: "r1", createdAt: nil, kind: .tool(Fixture.tool("t2"))),
            TimelineItem(id: "m1", runID: "r1", createdAt: nil,
                         kind: .assistantText(markdown: "answer", citations: [])),
            TimelineItem(id: "t3", runID: "r1", createdAt: nil, kind: .tool(Fixture.tool("t3"))),
        ]

        let rows = ChatTimelineRows.rows(from: items)

        #expect(rows.map(\.id) == ["tools-t1", "m1", "tools-t3"])
        // Text between calls splits the run into two groups (the web does the
        // same), and the group id is the first call's id (#433) — the same key
        // the disclosure store remembers the group's open state by.
    }

    @Test func reloadProducesTheSameRowIdentitiesSoDisclosureSurvives() throws {
        let store = ChatTimelineStore()
        let recorded = try Fixture.recorded()
        store.replaceHistory(recorded)
        let before = ChatTimelineRows.rows(from: store.items).map(\.id)

        store.replaceHistory(recorded)

        #expect(ChatTimelineRows.rows(from: store.items).map(\.id) == before)
    }

    // MARK: - Ported from ChatParityTests / ChatActivityParityTests

    @Test func thinkingWithoutAnIdAccumulatesIntoOneOpenSegment() {
        let store = ChatTimelineStore()
        store.apply(.activity(.thinking(id: nil, delta: "a")))
        store.apply(.activity(.thinking(id: nil, delta: "b")))

        #expect(store.items.count == 1)
        #expect(store.items.first?.kind == .reasoning(text: "ab", isLive: true, isOpen: true))
    }

    @Test func doneStopsTheShimmerWithoutEndingTheSegment() {
        let store = ChatTimelineStore()
        store.apply(.activity(.thinking(id: "s1", delta: "a")))
        store.apply(.activity(.thinkingDone(id: "s1")))
        store.apply(.activity(.thinking(id: "s1", delta: "b")))

        // The next delta before an activity boundary joins the same row.
        #expect(store.items.count == 1)
        #expect(store.items.first?.kind == .reasoning(text: "ab", isLive: true, isOpen: true))
    }

    @Test func anActivityBoundaryClosesTheReasoningSegment() {
        let store = ChatTimelineStore()
        store.apply(.activity(.thinking(id: "s1", delta: "think")))
        store.apply(.activity(.toolCall(ChatToolCallEvent(id: "t1", tool: "entity_search", arguments: "{}"))))

        #expect(store.items.first?.kind == .reasoning(text: "think", isLive: false, isOpen: false))
        #expect(Fixture.toolIDs(store.items) == ["t1"])
    }

    @Test func toolResultSettlesTheMatchingCallByIDWithItsDuration() throws {
        var clock = Date(timeIntervalSince1970: 100)
        let store = ChatTimelineStore(now: { clock })
        store.apply(.activity(.toolCall(ChatToolCallEvent(id: "t1", tool: "entity_search", arguments: "{}"))))
        clock = Date(timeIntervalSince1970: 102.5)
        store.apply(.activity(.toolResult(ChatToolResultEvent(
            id: "t1",
            tool: "entity_search",
            result: "{}",
            isError: false,
            outputSize: 2
        ))))

        let kind = try #require(store.items.first?.kind)
        guard case let .tool(info) = kind else {
            Issue.record("the call did not settle into a tool row")
            return
        }
        #expect(info.isRunning == false)
        #expect(info.duration == 2.5)
        #expect(info.responseSize == 2)
    }

    // MARK: - Loader: one load feeds both the baseline and the rows

    @Test func loadFillsTheTimelineFromTheHistoryEndpoint() async throws {
        let history = try Fixture.history()
        let recorded = ChatTimelineHistory.items(from: history)
        let session = SequencedSessionLogClient([.success([Fixture.record("hi")])])
        let historyClient = SequencedHistoryClient([.success(history)])
        let loader = ChatSessionHistoryLoader(client: session, historyClient: historyClient)
        let transcript = ChatSessionTranscriptStore()
        let timeline = ChatTimelineStore()

        let applied = await loader.load(conversationID: "conv-1", into: transcript, timeline: timeline)

        #expect(applied)
        #expect(historyClient.conversationIDs == ["conv-1"])
        #expect(timeline.isLoadingHistory == false)
        #expect(timeline.historyError == nil)
        #expect(Fixture.labels(timeline.items) == Fixture.labels(recorded))
    }

    @Test func historyFailureSurfacesOnTheTimelineWithoutLeavingItLoading() async {
        let session = SequencedSessionLogClient([.success([Fixture.record("hi")])])
        let historyClient = SequencedHistoryClient([.failure(GatewayError.transport("Session log unreachable"))])
        let loader = ChatSessionHistoryLoader(client: session, historyClient: historyClient)
        let transcript = ChatSessionTranscriptStore()
        let timeline = ChatTimelineStore()

        let applied = await loader.load(conversationID: "conv-1", into: transcript, timeline: timeline)

        #expect(applied == false)
        #expect(timeline.isLoadingHistory == false)
        #expect(timeline.historyError == "Session log unreachable")
        #expect(timeline.items.isEmpty)
    }
}

/// Serves one queued history per call, so a test can fail once and then
/// succeed, and records which conversations were asked for.
@MainActor
final class SequencedHistoryClient: ConversationHistoryFetching {
    private var results: [Result<ConversationHistory, Error>]
    private(set) var conversationIDs: [String] = []

    init(_ results: [Result<ConversationHistory, Error>]) {
        self.results = results
    }

    func history(conversationID: String) async throws -> ConversationHistory {
        conversationIDs.append(conversationID)
        let result = results.count > 1
            ? results.removeFirst()
            : (results.first ?? .success(ConversationHistory(items: [])))
        return try result.get()
    }
}
