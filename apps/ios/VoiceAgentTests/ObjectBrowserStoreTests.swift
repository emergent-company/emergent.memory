import Foundation
@testable import Memory
import Testing

/// A recording object-fetching fake. Serves canned pages in call order, with an
/// optional per-call delay so a slow first load can be overtaken by a newer one
/// in the stale-guard test.
///
/// Main-actor isolated (the app builds with `-default-isolation=MainActor` and
/// ``ObjectFetching`` is a main-actor protocol), so it is a class rather than an
/// actor; the store is reentrant across its `await`s either way.
@MainActor
final class FakeObjectClient: ObjectFetching {
    struct Call: Equatable, Sendable {
        let agentID: String
        let provenance: ObjectProvenance
        let cursor: String?
    }

    private let pages: [ObjectPage]
    private let delays: [Duration?]
    private let failure: Error?
    private var recorded: [Call] = []
    private var callCount = 0

    init(pages: [ObjectPage] = [], delays: [Duration?] = [], failure: Error? = nil) {
        self.pages = pages
        self.delays = delays
        self.failure = failure
    }

    func listObjects(agentID: String, provenance: ObjectProvenance, cursor: String?) async throws -> ObjectPage {
        let index = callCount
        callCount += 1
        recorded.append(Call(agentID: agentID, provenance: provenance, cursor: cursor))
        if index < delays.count, let delay = delays[index] {
            try await Task.sleep(for: delay)
        }
        if let failure {
            throw failure
        }
        return index < pages.count ? pages[index] : ObjectPage(items: [], nextCursor: nil)
    }

    func calls() -> [Call] {
        recorded
    }
}

@MainActor
struct ObjectBrowserStoreTests {
    private func page(_ ids: [String], cursor: String? = nil) -> ObjectPage {
        ObjectPage(
            items: ids.map { GraphObject(id: $0, type: "note", key: $0) },
            nextCursor: cursor
        )
    }

    @Test func loadPublishesFirstPage() async {
        let fake = FakeObjectClient(pages: [page(["o1", "o2"], cursor: "cur2")])
        let store = ObjectBrowserStore(client: fake)

        await store.load(agentID: "a1")

        #expect(store.loadState == .loaded)
        #expect(store.objects.map(\.id) == ["o1", "o2"])
        #expect(store.nextCursor == "cur2")
        #expect(store.hasMore)
        let calls = fake.calls()
        #expect(calls == [FakeObjectClient.Call(agentID: "a1", provenance: .any, cursor: nil)])
    }

    @Test func loadUsesSelectedProvenance() async {
        let fake = FakeObjectClient(pages: [page(["o1"])])
        let store = ObjectBrowserStore(client: fake)
        store.provenance = .created

        await store.load(agentID: "a1")

        let calls = fake.calls()
        #expect(calls.first?.provenance == .created)
    }

    @Test func loadFailureSetsFailedState() async {
        struct Boom: Error {}
        let fake = FakeObjectClient(failure: Boom())
        let store = ObjectBrowserStore(client: fake)

        await store.load(agentID: "a1")

        if case .failed = store.loadState {
            // expected
        } else {
            Issue.record("loadState = \(store.loadState), want .failed")
        }
        #expect(store.objects.isEmpty)
    }

    @Test func loadMoreAppendsNextPage() async {
        let fake = FakeObjectClient(pages: [
            page(["o1"], cursor: "c1"),
            page(["o2"]),
        ])
        let store = ObjectBrowserStore(client: fake)

        await store.load(agentID: "a1")
        #expect(store.hasMore)
        await store.loadMore(agentID: "a1")

        #expect(store.objects.map(\.id) == ["o1", "o2"])
        #expect(store.nextCursor == nil)
        #expect(store.hasMore == false)
        let calls = fake.calls()
        #expect(calls.count == 2)
        #expect(calls[1].cursor == "c1")
    }

    @Test func loadMoreWithoutCursorDoesNothing() async {
        let fake = FakeObjectClient(pages: [page(["o1"])])
        let store = ObjectBrowserStore(client: fake)

        await store.load(agentID: "a1")
        await store.loadMore(agentID: "a1")

        #expect(store.objects.map(\.id) == ["o1"])
        let calls = fake.calls()
        #expect(calls.count == 1)
    }

    @Test func staleLoadDoesNotClobberNewer() async throws {
        let fake = FakeObjectClient(
            pages: [page(["old"]), page(["new"])],
            delays: [.milliseconds(120), nil]
        )
        let store = ObjectBrowserStore(client: fake)

        let first = Task { await store.load(agentID: "a1") }
        try await Task.sleep(for: .milliseconds(20))
        let second = Task { await store.load(agentID: "a1") }
        _ = await first.value
        _ = await second.value

        #expect(store.objects.map(\.id) == ["new"])
        #expect(store.loadState == .loaded)
    }
}
