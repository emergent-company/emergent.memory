import Foundation
@testable import Memory
import Testing

/// A recording session-log-fetching fake, modeled on `FakeObjectClient`. Serves
/// canned list/timeline results and counts calls.
///
/// Main-actor isolated (the app builds with `-default-isolation=MainActor` and
/// ``SessionLogFetching`` is a main-actor protocol), so it is a class rather
/// than an actor.
/// Thrown by the fake when an archive is scripted to fail.
struct ArchiveFailure: Error {}

@MainActor
final class FakeSessionLogClient: SessionLogFetching {
    var sessionsResult: Result<[SessionSummary], Error>
    let timelineResult: Result<[SessionRecord], Error>
    /// Ids whose archive must fail, so partial-failure tests can pin rollback.
    var archiveFailures: Set<String> = []
    /// When false the fake returns `sessionsResult` verbatim, simulating a list
    /// refresh that lands before the server has caught up with an archive.
    var respectsArchiveFilter = true
    private(set) var listCallCount = 0
    private(set) var timelineCallCount = 0
    private(set) var listAgentIDs: [String?] = []
    private(set) var listIncludeArchived: [Bool] = []
    private(set) var archivedIDs: [String] = []
    private(set) var unarchivedIDs: [String] = []
    private(set) var deletedIDs: [String] = []

    init(
        sessions: Result<[SessionSummary], Error> = .success([]),
        timeline: Result<[SessionRecord], Error> = .success([])
    ) {
        sessionsResult = sessions
        timelineResult = timeline
    }

    func listSessions(agentID: String?, includeArchived: Bool) async throws -> [SessionSummary] {
        listCallCount += 1
        listAgentIDs.append(agentID)
        listIncludeArchived.append(includeArchived)
        let all = try sessionsResult.get()
        guard respectsArchiveFilter, !includeArchived else { return all }
        return all.filter { !archivedIDs.contains($0.id) }
    }

    func detail(room: String) async throws -> SessionDetail {
        try SessionDetail(room: room, records: timelineResult.get())
    }

    func timeline(room _: String) async throws -> [SessionRecord] {
        timelineCallCount += 1
        return try timelineResult.get()
    }

    func archiveConversation(id: String) async throws {
        if archiveFailures.contains(id) {
            throw ArchiveFailure()
        }
        archivedIDs.append(id)
    }

    func unarchiveConversation(id: String) async throws {
        unarchivedIDs.append(id)
        archivedIDs.removeAll { $0 == id }
    }

    func deleteConversation(id: String) async throws {
        deletedIDs.append(id)
    }

    func respondQuestion(questionID: String, response: String, message: String) async throws {
        _ = questionID
        _ = response
        _ = message
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

    @Test func loadPassesAgentFilter() async {
        let fake = FakeSessionLogClient(sessions: .success([summary("r1")]))
        let store = SessionsStore(client: fake)

        await store.load(agentID: "agent-a")

        #expect(fake.listAgentIDs == ["agent-a"])
    }

    @Test func lifecycleActionsReloadWithLastFilter() async {
        let fake = FakeSessionLogClient(sessions: .success([summary("r1")]))
        let store = SessionsStore(client: fake)
        await store.load(agentID: "agent-a", includeArchived: true)

        await store.archive(id: "conv-1")
        await store.unarchive(id: "conv-2")
        await store.delete(id: "conv-3")

        #expect(fake.archivedIDs == ["conv-1"])
        #expect(fake.unarchivedIDs == ["conv-2"])
        #expect(fake.deletedIDs == ["conv-3"])
        // One initial load + one reload per lifecycle action, preserving both
        // the agent filter and archived visibility.
        #expect(fake.listCallCount == 4)
        #expect(fake.listAgentIDs == ["agent-a", "agent-a", "agent-a", "agent-a"])
        #expect(fake.listIncludeArchived == [true, true, true, true])
        #expect(store.includeArchived == true)
    }

    @Test func setIncludeArchivedIsUsedByNextLoad() async {
        let fake = FakeSessionLogClient(sessions: .success([summary("r1")]))
        let store = SessionsStore(client: fake)

        store.setIncludeArchived(true)
        await store.load(agentID: "agent-a")

        #expect(store.includeArchived == true)
        #expect(fake.listIncludeArchived == [true])
    }

    // MARK: - Sub-chat grouping

    @Test func threadsNestChildrenUnderTheirParent() {
        let parent = summary("parent")
        var child = summary("child")
        child.parentConversationID = "parent"
        let other = summary("other")

        let threads = SessionThreading.threads(from: [parent, child, other])

        #expect(threads.map(\.id) == ["parent", "other"])
        #expect(threads[0].children.map(\.id) == ["child"])
        #expect(threads[0].childCount == 1)
        #expect(threads[1].children.isEmpty)
    }

    @Test func threadsPromoteAnOrphanChildToRoot() {
        var orphan = summary("orphan")
        orphan.parentConversationID = "missing"

        let threads = SessionThreading.threads(from: [orphan])

        #expect(threads.map(\.id) == ["orphan"])
        #expect(threads[0].children.isEmpty)
    }

    @Test func threadsKeepParentVisibleWhenOnlyAChildMatches() {
        let parent = summary("parent")
        var child = summary("child")
        child.parentConversationID = "parent"
        let other = summary("other")

        let threads = SessionThreading.threads(from: [parent, child, other]) { $0.room == "child" }

        #expect(threads.map(\.id) == ["parent"])
        #expect(threads[0].children.map(\.id) == ["child"])
    }

    // MARK: - Bulk archive

    @Test func bulkArchiveHidesRowsAndRestoresOnlyFailures() async {
        let fake = FakeSessionLogClient(sessions: .success([summary("a"), summary("b"), summary("c")]))
        fake.archiveFailures = ["b"]
        let store = SessionsStore(client: fake)
        await store.load()

        let result = await store.archive(ids: ["a", "b"])

        #expect(result.requested == ["a", "b"])
        #expect(result.failed == ["b"])
        #expect(fake.archivedIDs == ["a"])
        // The success stays archived; only the failed row comes back.
        #expect(store.sessions.map(\.room) == ["b", "c"])
    }

    @Test func bulkArchiveRefreshCannotResurrectAHiddenRow() async {
        // `respectsArchiveFilter = false` models a list refresh that still
        // includes the row while the archive is settling (or a lagging server).
        let fake = FakeSessionLogClient(sessions: .success([summary("a"), summary("b")]))
        fake.respectsArchiveFilter = false
        let store = SessionsStore(client: fake)
        await store.load()
        await store.archive(ids: ["a"])

        fake.sessionsResult = .success([summary("a"), summary("b")])
        await store.load()

        #expect(store.sessions.map(\.room) == ["b"])
    }
}
