import Foundation
@testable import Memory
import Testing

/// A recording session-log-fetching fake, modeled on `FakeObjectClient`. Serves
/// canned list/timeline results and counts calls.
///
/// Main-actor isolated (the app builds with `-default-isolation=MainActor` and
/// ``SessionLogFetching`` is a main-actor protocol), so it is a class rather
/// than an actor.
@MainActor
final class FakeSessionLogClient: SessionLogFetching {
    let sessionsResult: Result<[SessionSummary], Error>
    let timelineResult: Result<[SessionRecord], Error>
    private(set) var listCallCount = 0
    private(set) var timelineCallCount = 0

    init(
        sessions: Result<[SessionSummary], Error> = .success([]),
        timeline: Result<[SessionRecord], Error> = .success([])
    ) {
        sessionsResult = sessions
        timelineResult = timeline
    }

    func listSessions() async throws -> [SessionSummary] {
        listCallCount += 1
        return try sessionsResult.get()
    }

    func timeline(room _: String) async throws -> [SessionRecord] {
        timelineCallCount += 1
        return try timelineResult.get()
    }
}

@MainActor
struct SessionsStoreTests {
    private func summary(_ room: String) -> SessionSummary {
        SessionSummary(
            room: room,
            startedAt: "10:00:00.000",
            endedAt: nil,
            turns: 2,
            toolCalls: 0,
            preview: "hello"
        )
    }

    @Test func loadPublishesSessions() async {
        let first = summary("r1")
        let second = summary("r2")
        let fake = FakeSessionLogClient(sessions: .success([first, second]))
        let store = SessionsStore(client: fake)

        await store.load()

        #expect(store.loadState == .loaded)
        #expect(store.sessions.map(\.room) == ["r1", "r2"])
        #expect(fake.listCallCount == 1)
    }

    @Test func loadFailureSetsFailedState() async {
        struct Boom: Error {}
        let fake = FakeSessionLogClient(sessions: .failure(Boom()))
        let store = SessionsStore(client: fake)

        await store.load()

        if case .failed = store.loadState {
            // expected
        } else {
            Issue.record("loadState = \(store.loadState), want .failed")
        }
        #expect(store.sessions.isEmpty)
    }

    @Test func detailLoadPublishesRecords() async throws {
        let record = try JSONDecoder().decode(
            SessionRecord.self,
            from: Data(#"{"kind":"turn","role":"user","text":"hello"}"#.utf8)
        )
        let fake = FakeSessionLogClient(timeline: .success([record]))
        let store = SessionDetailStore(client: fake)

        await store.load(room: "r1")

        #expect(store.loadState == .loaded)
        #expect(store.records.count == 1)
        #expect(store.failureMessage == nil)
        #expect(fake.timelineCallCount == 1)
    }

    @Test func detailLoadFailureIsEmptyTimelineNotError() async {
        struct Boom: Error {}
        let fake = FakeSessionLogClient(timeline: .failure(Boom()))
        let store = SessionDetailStore(client: fake)

        await store.load(room: "r1")

        // A missing/unreachable session renders as an empty timeline, not a
        // hard error: still `.loaded`, with the failure text carried aside.
        #expect(store.loadState == .loaded)
        #expect(store.records.isEmpty)
        #expect(store.failureMessage != nil)
    }
}
