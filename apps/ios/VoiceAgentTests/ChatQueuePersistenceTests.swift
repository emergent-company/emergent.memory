import Foundation
@testable import Memory
import Testing

// MARK: - Composer queue persistence

// Storage edge cases for the composer queue (`emergent.memory#438`): a queue that
// cannot be decoded must degrade to empty rather than take the chat down, and a
// session with no project id must not invent a key two projects would share.

@MainActor
private func makePersistenceDefaults() -> UserDefaults {
    let suite = "ChatQueuePersistenceTests.\(UUID().uuidString)"
    let defaults = UserDefaults(suiteName: suite)!
    defaults.removePersistentDomain(forName: suite)
    return defaults
}

@MainActor struct ChatQueuePersistenceTests {
    private let conversationID = "6f1c2a44-0000-4000-8000-0000000000c2"

    /// A truncated or foreign payload is not a reason to lose the chat: the
    /// queue reads as empty and the next write replaces the bad slot.
    @Test func corruptQueueLoadsEmpty() throws {
        let defaults = makePersistenceDefaults()
        let key = try #require(ChatQueueKey(projectID: "proj-a", conversationID: conversationID))
        defaults.set(Data(#"{"items":[{"id":"x""#.utf8), forKey: key.storageKey)

        let queue = ChatQueueStore(defaults: defaults)
        queue.scope(projectID: "proj-a", agentName: "alfred", conversationID: conversationID)

        #expect(queue.items.isEmpty)
        #expect(!queue.armed)

        // Writing over the corrupt slot recovers without a relaunch.
        queue.enqueue("after the damage", whileRunning: true)
        let reopened = ChatQueueStore(defaults: defaults)
        reopened.scope(projectID: "proj-a", agentName: "alfred", conversationID: conversationID)
        #expect(reopened.items.map(\.text) == ["after the damage"])
    }

    /// No project id means no key at all: an empty scope would make every
    /// project share one slot. The rows stay in memory, but nothing is written
    /// and the pump refuses to send them.
    @Test func noProjectIDMeansNoKeyAndNoPumping() {
        let defaults = makePersistenceDefaults()
        let queue = ChatQueueStore(defaults: defaults)
        queue.scope(projectID: "", agentName: "alfred", conversationID: conversationID)

        #expect(queue.key == nil)
        #expect(!queue.isScoped)

        queue.enqueue("follow up", whileRunning: true)
        #expect(queue.items.count == 1)

        var sent: [String] = []
        queue.pump(whileRunning: false) { sent.append($0); return true }
        #expect(sent.isEmpty)

        // Nothing landed in a shared slot, so another id-less project is empty.
        let other = ChatQueueStore(defaults: defaults)
        other.scope(projectID: "", agentName: "alfred", conversationID: conversationID)
        #expect(other.items.isEmpty)
        #expect(defaults.object(forKey: "memory.chat.queue.v1..\(conversationID)") == nil)
    }
}
