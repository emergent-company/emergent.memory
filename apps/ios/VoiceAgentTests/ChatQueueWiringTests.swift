import Foundation
@testable import Memory
import Testing

// MARK: - Turn end → the composer queue, wired

// The full path behind `emergent.memory#438`: a turn ends, the transport calls
// its hook, the session's pump takes one armed row and sends it. The store's
// rules and the transport's notification are covered separately; this file
// drives them together, which is also the only place that shows the hook's
// target is still alive after `ChatSessionView.start()` returned.
//
// A hook target held only by a local in `start()` is released the moment the
// task yields, and every turn end becomes a silent no-op: the tests here fail
// on that shape, so the wiring cannot rot back into it.

/// One wired chat: the transport whose hook the pump owns, the stream the test
/// drives frame by frame, and the stores the turn and the queue share.
@MainActor
private struct WiredChat {
    let stream: InFlightChatStream
    let transport: HTTPChatTransport
    let queue: ChatQueueStore
    let activity: ChatActivityStore
    let transcript: ChatSessionTranscriptStore
    /// Held here exactly as `ChatSessionView` holds it in `@State`: the hook only
    /// points at the pump weakly, so this reference is what keeps it alive.
    let pump: ChatQueuePump

    /// Sends one turn and waits until its stream is open.
    func startTurn(_ text: String) async -> Task<Void, Never>? {
        let task = transport.send(
            text,
            conversationID: "conv-1",
            agentName: "",
            transcript: transcript,
            activity: activity
        )
        await waitForQueueWiring { stream.continuation != nil }
        return task
    }

    /// Waits until the pump has started a turn the test has no task handle for.
    func waitForReleasedTurn() async {
        await waitForQueueWiring { stream.continuation != nil }
    }

    /// Ends the turn in flight — `done`, then the stream closing, as the gateway
    /// does — and returns once its task finished, so a release the hook started
    /// has already happened.
    func endTurn(_ task: Task<Void, Never>) async {
        stream.yield(ChatStreamFixtures.done)
        stream.finish()
        await task.value
    }

    /// The user lines the transport has put on screen, in order.
    var userTexts: [String] {
        transcript.liveMessages.filter { $0.role == .user }.map(\.text)
    }
}

@MainActor
private func makeWiredChat(alsoOnTurnFinished: (() -> Void)? = nil) -> WiredChat {
    let suite = "ChatQueueWiringTests.\(UUID().uuidString)"
    let defaults = UserDefaults(suiteName: suite)!
    defaults.removePersistentDomain(forName: suite)
    let queue = ChatQueueStore(defaults: defaults)
    queue.scope(projectID: "proj-a", agentName: "alfred", conversationID: "conv-1")
    let stream = InFlightChatStream()
    let transport = HTTPChatTransport(config: MemoryConfig(), stream: stream)
    let activity = ChatActivityStore()
    let transcript = ChatSessionTranscriptStore()
    // The sender is a temporary on purpose, as `attachQueuePump()` hands one
    // over: whatever keeps it alive has to be the pump the view retains.
    let sender = ChatQueueSender(
        transport: transport,
        activity: activity,
        transcript: transcript,
        activeSession: ActiveSessionStore(),
        route: ChatRoute(agentName: "", conversationID: "conv-1", title: nil)
    )
    let pump = ChatQueuePump.install(
        on: transport,
        queue: queue,
        sender: sender,
        alsoOnTurnFinished: alsoOnTurnFinished
    )
    return WiredChat(
        stream: stream,
        transport: transport,
        queue: queue,
        activity: activity,
        transcript: transcript,
        pump: pump
    )
}

/// Pumps the main actor until `condition` holds. No wall-clock sleep: each yield
/// lets the turn task consume the next frame.
@MainActor
private func waitForQueueWiring(_ condition: () -> Bool) async {
    for _ in 0 ..< 1000 {
        if condition() {
            return
        }
        await Task.yield()
    }
}

@MainActor struct ChatQueueWiringTests {
    /// The headline behaviour: a row queued behind a running turn goes out when
    /// that turn ends. This is the test that fails when the hook's target is a
    /// temporary — the shape that shipped in d419981, where the sender died with
    /// `start()`.
    @Test func finishedTurnReleasesTheQueuedRow() async throws {
        let chat = makeWiredChat()
        let turn = try #require(await chat.startTurn("hello"))
        chat.queue.enqueue("follow up", whileRunning: true)

        await chat.endTurn(turn)

        #expect(chat.queue.items.isEmpty)
        #expect(chat.userTexts == ["hello", "follow up"])
        // The row became a real turn, not just a store mutation.
        #expect(chat.transport.isStreaming)
        chat.transport.reset()
    }

    /// A finished turn runs the pump's other listener too (the plan card's
    /// refresh, `emergent.memory#440`) — exactly once, and still after the
    /// queue's own release.
    @Test func finishedTurnAlsoRunsTheSecondListener() async throws {
        var planRefreshes = 0
        let chat = makeWiredChat(alsoOnTurnFinished: { planRefreshes += 1 })
        let turn = try #require(await chat.startTurn("hello"))

        await chat.endTurn(turn)

        #expect(planRefreshes == 1)
        chat.transport.reset()
    }

    /// Two rows, two turn ends: one pump per turn end, never a cascade.
    @Test func oneRowPerTurnEnd() async throws {
        let chat = makeWiredChat()
        let turn = try #require(await chat.startTurn("hello"))
        chat.queue.enqueue("one", whileRunning: true)
        chat.queue.enqueue("two", whileRunning: true)

        await chat.endTurn(turn)

        #expect(chat.userTexts == ["hello", "one"])
        #expect(chat.queue.items.map(\.text) == ["two"])

        // End the released row's turn: the second row follows it.
        await chat.waitForReleasedTurn()
        chat.stream.yield(ChatStreamFixtures.done)
        chat.stream.finish()
        await waitForQueueWiring { chat.userTexts.count == 3 }

        #expect(chat.userTexts == ["hello", "one", "two"])
        #expect(chat.queue.items.isEmpty)
        chat.transport.reset()
    }

    /// A run parked on a decision holds the queue even though nothing streams;
    /// answering it and then ending the run releases the row.
    @Test func parkedDecisionHoldsTheQueueThenReleasesIt() async throws {
        let chat = makeWiredChat()
        let turn = try #require(await chat.startTurn("hello"))
        chat.queue.enqueue("follow up", whileRunning: true)
        chat.stream.yield(ChatStreamFixtures.approval)
        await waitForQueueWiring { chat.activity.liveTurn?.isWaitingOnUser == true }

        await chat.endTurn(turn)

        // The turn ended, but its run is waiting on the user: nothing goes out.
        #expect(chat.userTexts == ["hello"])
        #expect(chat.queue.items.map(\.text) == ["follow up"])
        #expect(!chat.transport.isStreaming)

        // The user answers; the run then ends and the row is released.
        chat.activity.submitApproval(questionId: "q-approve", decision: .approve)
        chat.transport.onTurnFinished?()
        #expect(chat.userTexts == ["hello", "follow up"])
        #expect(chat.queue.items.isEmpty)
        chat.transport.reset()
    }

    /// An older parked run still blocks: reading only the newest turn let a
    /// second message mask it.
    @Test func anOlderParkedRunStillBlocks() {
        let chat = makeWiredChat()
        chat.activity.userTurnStarted(anchorMessageID: "m1")
        chat.activity.apply(.approval(ChatApprovalEvent(
            questionId: "q1",
            tool: "delete_document",
            arguments: "{}"
        )))
        chat.activity.userTurnStarted(anchorMessageID: "m2")

        let sender = ChatQueueSender(
            transport: chat.transport,
            activity: chat.activity,
            transcript: chat.transcript,
            activeSession: ActiveSessionStore(),
            route: ChatRoute(agentName: "", conversationID: "conv-1", title: nil)
        )
        #expect(sender.isBlocked)
    }
}
