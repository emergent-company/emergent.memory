import Foundation
@testable import Memory
import Testing

/// Serves one queued plan result per fetch, so a test can fail once and then
/// succeed, and records the conversation each fetch asked for.
///
/// Main-actor isolated like the app target's default isolation and the
/// ``SessionTodoFetching`` seam it implements, so it is a class rather than an
/// actor — the same shape as the session-log fake.
@MainActor
final class FakeSessionTodoFetcher: SessionTodoFetching {
    private var results: [Result<[SessionTodo], Error>]
    private(set) var conversationIDs: [String] = []

    init(_ results: [Result<[SessionTodo], Error>]) {
        self.results = results
    }

    func todos(conversationID: String) async throws -> [SessionTodo] {
        conversationIDs.append(conversationID)
        // The last queued result repeats, so a test only queues what changes.
        let result = results.count > 1 ? results.removeFirst() : (results.first ?? .success([]))
        return try result.get()
    }
}

/// The plan card's store (`emergent.memory#440`): the list the card renders, the
/// done count it shows while collapsed, and what a failed or superseded fetch
/// does to it.
@MainActor
struct ChatPlanStoreTests {
    private func todo(
        id: String,
        status: String,
        order: Int,
        content: String = "an item"
    ) -> SessionTodo {
        SessionTodo(id: id, content: content, status: status, order: order)
    }

    /// Pumps the main actor until `condition` holds, like the events tests: the
    /// store's `refresh()` owns its request, so a test cannot await it directly.
    private func waitUntil(_ condition: () -> Bool) async {
        for _ in 0 ..< 1000 {
            if condition() {
                return
            }
            await Task.yield()
        }
    }

    @Test func loadRendersThePlanInOrderAndCountsTheDoneItems() async {
        let fake = FakeSessionTodoFetcher([.success([
            todo(id: "t3", status: "pending", order: 3),
            todo(id: "t1", status: "completed", order: 1),
            todo(id: "t2", status: "in_progress", order: 2),
        ])])
        let store = ChatPlanStore(client: fake)

        await store.load(conversationID: "conv-1")

        #expect(store.conversationID == "conv-1")
        #expect(store.todos.map(\.id) == ["t1", "t2", "t3"])
        #expect(store.doneCount == 1)
        #expect(fake.conversationIDs == ["conv-1"])
    }

    /// The card draws glyphs for pending / in_progress / completed and shows
    /// anything else as its raw text, so the store must keep the server's string.
    @Test func unknownStatusesAreKeptVerbatim() async {
        let fake = FakeSessionTodoFetcher([.success([todo(id: "t1", status: "blocked", order: 1)])])
        let store = ChatPlanStore(client: fake)

        await store.load(conversationID: "conv-1")

        #expect(store.todos.count == 1)
        #expect(store.todos[0].status == "blocked")
        #expect(store.todos[0].knownStatus == nil)
        #expect(store.doneCount == 0)
    }

    /// The card is decoration: a fetch failure must leave the plan on screen
    /// rather than flash the card away and put it back a frame later.
    @Test func failedFetchKeepsTheLastGoodPlan() async {
        let fake = FakeSessionTodoFetcher([
            .success([todo(id: "t1", status: "pending", order: 1)]),
            .failure(GatewayError.transport("offline")),
        ])
        let store = ChatPlanStore(client: fake)

        await store.load(conversationID: "conv-1")
        await store.load(conversationID: "conv-1")

        #expect(fake.conversationIDs == ["conv-1", "conv-1"])
        #expect(store.todos.map(\.id) == ["t1"])
    }

    /// One chat must never show another's plan, even when the new conversation's
    /// fetch fails: the previous plan is dropped the moment the conversation
    /// changes.
    @Test func anotherConversationDropsThePreviousPlanFirst() async {
        let fake = FakeSessionTodoFetcher([
            .success([todo(id: "t1", status: "pending", order: 1)]),
            .failure(GatewayError.transport("offline")),
        ])
        let store = ChatPlanStore(client: fake)

        await store.load(conversationID: "conv-1")
        await store.load(conversationID: "conv-2")

        #expect(store.conversationID == "conv-2")
        #expect(store.todos.isEmpty)
    }

    /// A brand-new chat has no conversation id until its stream's `meta` frame:
    /// the store clears rather than fetching, and the card renders nothing.
    @Test func noConversationClearsThePlan() async {
        let fake = FakeSessionTodoFetcher([.success([todo(id: "t1", status: "pending", order: 1)])])
        let store = ChatPlanStore(client: fake)

        await store.load(conversationID: "conv-1")
        await store.load(conversationID: nil)

        #expect(store.conversationID == nil)
        #expect(store.todos.isEmpty)
        #expect(fake.conversationIDs == ["conv-1"])
    }

    /// The refresh frame and the turn-end hook both go through `refresh()`: it
    /// refetches the conversation the store is already showing.
    @Test func refreshRefetchesTheCurrentConversation() async {
        let fake = FakeSessionTodoFetcher([
            .success([todo(id: "t1", status: "pending", order: 1)]),
            .success([
                todo(id: "t1", status: "completed", order: 1),
                todo(id: "t2", status: "pending", order: 2),
            ]),
        ])
        let store = ChatPlanStore(client: fake)

        await store.load(conversationID: "conv-1")
        store.refresh()
        await waitUntil { store.todos.count == 2 }

        #expect(fake.conversationIDs == ["conv-1", "conv-1"])
        #expect(store.doneCount == 1)
        #expect(store.todos.map(\.id) == ["t1", "t2"])
    }

    /// With no conversation there is nothing to refresh, and no request may go
    /// out for an id the store never had.
    @Test func refreshWithoutAConversationDoesNothing() async {
        let fake = FakeSessionTodoFetcher([.success([])])
        let store = ChatPlanStore(client: fake)

        store.refresh()
        await Task.yield()

        #expect(fake.conversationIDs.isEmpty)
    }
}

/// The plan route's decode contract: `{"todos":[{"id","content","status","order"}]}`.
struct SessionTodosDecodingTests {
    @Test func decodesTheGatewayContract() throws {
        let body = #"""
        {"todos":[{"id":"t1","content":"Write tests","status":"in_progress","order":1}]}
        """#
        let response = try JSONDecoder().decode(SessionTodosResponse.self, from: Data(body.utf8))

        #expect(response.todos.count == 1)
        #expect(response.todos[0].id == "t1")
        #expect(response.todos[0].content == "Write tests")
        #expect(response.todos[0].status == "in_progress")
        #expect(response.todos[0].order == 1)
        #expect(response.todos[0].knownStatus?.rawValue == "in_progress")
    }

    /// The gateway answers every lookup failure with `{"todos":[]}`, so the
    /// empty plan is the normal "no session / no todos" body.
    @Test func decodesTheEmptyPlan() throws {
        let response = try JSONDecoder().decode(
            SessionTodosResponse.self,
            from: Data(#"{"todos":[]}"#.utf8)
        )

        #expect(response.todos.isEmpty)
    }
}
