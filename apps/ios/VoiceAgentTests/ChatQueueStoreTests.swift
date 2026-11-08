import Foundation
@testable import Memory
import Testing

// MARK: - Composer queue

// The queue's release rules (`emergent.memory#438`), ported from the web
// chat's `enqueue` / `pumpQueue` / `sendNow` / `maybeReleaseOnReopen`. The
// load-bearing one is arming: a message parked *behind a running turn* is
// released when that turn ends, while one parked with the composer idle (⌘↩)
// waits for an explicit action and must survive an unrelated turn end.

@MainActor
private func makeDefaults() -> UserDefaults {
    let suite = "ChatQueueStoreTests.\(UUID().uuidString)"
    let defaults = UserDefaults(suiteName: suite)!
    defaults.removePersistentDomain(forName: suite)
    return defaults
}

/// Serialized because each test owns a throwaway `UserDefaults` suite, but the
/// persistence reads/writes are shared process-wide state.
@Suite(.serialized) @MainActor struct ChatQueueStoreTests {
    private let conversationID = "6f1c2a44-0000-4000-8000-0000000000c1"

    private func makeQueue(
        defaults: UserDefaults,
        projectID: String = "proj-a",
        agentName: String = "alfred",
        conversationID: String?
    ) -> ChatQueueStore {
        let queue = ChatQueueStore(defaults: defaults)
        queue.scope(projectID: projectID, agentName: agentName, conversationID: conversationID)
        return queue
    }

    /// The bug this issue exists to prevent: a message parked while a turn is
    /// streaming must arm the queue, so the turn's end releases it.
    @Test func enqueueWhileRunningArmsTheQueue() {
        let defaults = makeDefaults()
        let queue = makeQueue(defaults: defaults, conversationID: conversationID)

        queue.enqueue("second thought", whileRunning: true)

        #expect(queue.items.map(\.text) == ["second thought"])
        #expect(queue.armed)

        // The queue is durable: reopening the view reads the same armed queue.
        let reopened = makeQueue(defaults: defaults, conversationID: conversationID)
        #expect(reopened.items.map(\.text) == ["second thought"])
        #expect(reopened.armed)
    }

    /// ⌘↩ when idle stages a message on purpose; it is not released by a later
    /// unrelated turn ending.
    @Test func idleEnqueueDoesNotArmTheQueue() {
        let defaults = makeDefaults()
        let queue = makeQueue(defaults: defaults, conversationID: conversationID)

        queue.enqueue("park it", whileRunning: false)

        #expect(queue.items.map(\.text) == ["park it"])
        #expect(!queue.armed)

        var sent: [String] = []
        queue.pump(whileRunning: false) { sent.append($0); return true }
        #expect(sent.isEmpty)
    }

    /// A turn end releases one item, and only when nothing is streaming.
    @Test func turnEndPumpsExactlyOneItem() {
        let defaults = makeDefaults()
        let queue = makeQueue(defaults: defaults, conversationID: conversationID)
        queue.enqueue("one", whileRunning: true)
        queue.enqueue("two", whileRunning: true)

        var sent: [String] = []
        let dispatch: (String) -> Bool = { sent.append($0); return true }

        // Still streaming: the pump refuses.
        queue.pump(whileRunning: true, dispatch: dispatch)
        #expect(sent.isEmpty)

        queue.pump(whileRunning: false, dispatch: dispatch)
        #expect(sent == ["one"])
        #expect(queue.items.map(\.text) == ["two"])
        #expect(queue.armed)
    }

    /// The last item clearing disarms the queue, so a later idle enqueue is not
    /// released by the next turn end.
    @Test func emptyQueueDisarmsAndStaysPut() {
        let defaults = makeDefaults()
        let queue = makeQueue(defaults: defaults, conversationID: conversationID)
        queue.enqueue("only", whileRunning: true)

        var sent: [String] = []
        queue.pump(whileRunning: false) { sent.append($0); return true }
        #expect(sent == ["only"])
        #expect(queue.items.isEmpty)
        #expect(!queue.armed)

        queue.enqueue("idle thought", whileRunning: false)
        queue.pump(whileRunning: false) { sent.append($0); return true }
        #expect(sent == ["only"])
    }

    /// A failed send puts the item back at the head instead of losing it.
    @Test func failedSendPutsTheItemBackAtTheHead() {
        let defaults = makeDefaults()
        let queue = makeQueue(defaults: defaults, conversationID: conversationID)
        queue.enqueue("one", whileRunning: true)
        queue.enqueue("two", whileRunning: true)

        queue.pump(whileRunning: false) { _ in false }
        #expect(queue.items.map(\.text) == ["one", "two"])
    }

    /// Send next while idle sends one row and disarms the rest (#1301): the
    /// turn it starts must not cascade-release them.
    @Test func sendNextWhileIdleDisarmsTheRest() {
        let defaults = makeDefaults()
        let queue = makeQueue(defaults: defaults, conversationID: conversationID)
        queue.enqueue("first", whileRunning: true)
        queue.enqueue("second", whileRunning: true)

        var sent: [String] = []
        let dispatch: (String) -> Bool = { sent.append($0); return true }
        queue.sendNext(secondID(in: queue), whileRunning: false, dispatch: dispatch)

        #expect(sent == ["second"])
        #expect(queue.items.map(\.text) == ["first"])
        #expect(!queue.armed)

        queue.pump(whileRunning: false, dispatch: dispatch)
        #expect(sent == ["second"])
    }

    /// Send next while running promotes the row to the head and arms the queue;
    /// it goes out first when the turn ends.
    @Test func sendNextWhileRunningPromotesToTheHead() {
        let defaults = makeDefaults()
        let queue = makeQueue(defaults: defaults, conversationID: conversationID)
        queue.enqueue("first", whileRunning: true)
        queue.enqueue("second", whileRunning: false)

        var sent: [String] = []
        let dispatch: (String) -> Bool = { sent.append($0); return true }
        queue.sendNext(secondID(in: queue), whileRunning: true, dispatch: dispatch)

        #expect(sent.isEmpty)
        #expect(queue.items.map(\.text) == ["second", "first"])
        #expect(queue.armed)

        queue.pump(whileRunning: false, dispatch: dispatch)
        #expect(sent == ["second"])
    }

    /// A chat with no id yet stores under `__new__`; learning the conversation
    /// id moves the queue so it is never stranded or delivered elsewhere.
    @Test func newChatQueueMovesToTheConversationKey() {
        let defaults = makeDefaults()
        let newChat = makeQueue(defaults: defaults, conversationID: nil)
        newChat.enqueue("staged", whileRunning: true)

        newChat.learnConversationID(conversationID)

        let reopened = makeQueue(defaults: defaults, conversationID: conversationID)
        #expect(reopened.items.map(\.text) == ["staged"])
        #expect(reopened.armed)

        // The `__new__` slot is gone: a later new chat for the same agent must
        // not inherit this conversation's rows.
        let laterNewChat = makeQueue(defaults: defaults, conversationID: nil)
        #expect(laterNewChat.items.isEmpty)
        #expect(!laterNewChat.armed)
    }

    /// Queues are scoped per project: a row typed in one project never shows up
    /// in another, even for the same conversation id.
    @Test func keysAreScopedPerProject() {
        let defaults = makeDefaults()
        let projectA = makeQueue(defaults: defaults, projectID: "proj-a", conversationID: conversationID)
        projectA.enqueue("a-only", whileRunning: true)

        let projectB = makeQueue(defaults: defaults, projectID: "proj-b", conversationID: conversationID)
        #expect(projectB.items.isEmpty)
        #expect(!projectB.armed)

        let newChatB = makeQueue(defaults: defaults, projectID: "proj-b", conversationID: nil)
        #expect(newChatB.items.isEmpty)
    }

    /// Reopening a chat releases an armed queue only when nothing is streaming
    /// and its last run has ended.
    @Test func releaseOnReopenNeedsAnEndedRunAndAnIdleComposer() {
        let defaults = makeDefaults()
        let queue = makeQueue(defaults: defaults, conversationID: conversationID)
        queue.enqueue("waiting", whileRunning: true)

        var sent: [String] = []
        let dispatch: (String) -> Bool = { sent.append($0); return true }

        queue.releaseOnReopen(whileRunning: true, hasConversation: true, lastRunEnded: true, dispatch: dispatch)
        #expect(sent.isEmpty)

        queue.releaseOnReopen(whileRunning: false, hasConversation: false, lastRunEnded: true, dispatch: dispatch)
        #expect(sent.isEmpty)

        queue.releaseOnReopen(whileRunning: false, hasConversation: true, lastRunEnded: false, dispatch: dispatch)
        #expect(sent.isEmpty)

        queue.releaseOnReopen(whileRunning: false, hasConversation: true, lastRunEnded: true, dispatch: dispatch)
        #expect(sent == ["waiting"])
        #expect(queue.items.isEmpty)
    }

    /// Edits and removals are per row and persist.
    @Test func rowsCanBeEditedAndRemoved() {
        let defaults = makeDefaults()
        let queue = makeQueue(defaults: defaults, conversationID: conversationID)
        queue.enqueue("first", whileRunning: false)
        queue.enqueue("second", whileRunning: false)

        let firstID = queue.items[0].id
        queue.updateText("first, edited", for: firstID)
        queue.remove(secondID(in: queue))

        let reopened = makeQueue(defaults: defaults, conversationID: conversationID)
        #expect(reopened.items.map(\.text) == ["first, edited"])
    }

    /// A blank row is not a message: it never enters the queue (the web drops it
    /// on enqueue and on load).
    @Test func blankTextIsNotQueued() {
        let defaults = makeDefaults()
        let queue = makeQueue(defaults: defaults, conversationID: conversationID)
        queue.enqueue("   \n ", whileRunning: true)
        #expect(queue.items.isEmpty)
        #expect(!queue.armed)
    }

    private func secondID(in queue: ChatQueueStore) -> String {
        queue.items[1].id
    }
}
