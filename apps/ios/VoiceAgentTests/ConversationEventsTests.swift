import Foundation
@testable import Memory
import Testing

// MARK: - Decoder

/// The conversation events stream's frames (`GET /api/conversations/:id/events`):
/// the populated and bare `refresh` shapes, and the retained `live_replay`
/// frames the reattach path renders (`emergent.memory#437`, `emergent.memory#470`).
struct ConversationRefreshDecoderTests {
    private func decode(_ payload: String) -> ChatStreamEvent? {
        ChatStreamFrameDecoder.decode(payload, fallbackToolID: { "fallback" })
    }

    @Test func refreshFrameCarriesRunFacts() {
        let payload = #"{"type":"refresh","bucket":"running","runId":"run-1","runStatus":"working","pendingApprovals":1,"pendingQuestions":2}"#
        #expect(decode(payload) == .refresh(ConversationRunState(
            bucket: "running",
            runID: "run-1",
            runStatus: "working",
            pendingApprovals: 1,
            pendingQuestions: 2
        )))
    }

    /// The connect-time frame has no facts: dropping it is what keeps it from
    /// clearing a turn this client is already streaming.
    @Test func bareRefreshFrameIsDropped() {
        #expect(decode(#"{"type":"refresh"}"#) == nil)
    }

    @Test func liveReplayDecodesThinkingAndRunningTool() {
        let payload = #"{"type":"live_replay","runId":"run-1","frames":[{"type":"thinking","id":"t1","text":"Checking","done":false},{"type":"mcp_tool","tool":"search","status":"started","id":"call-1"}]}"#
        guard case let .replay(events)? = decode(payload) else {
            Issue.record("expected a replay frame")
            return
        }
        #expect(events.count == 2)
        guard case let .activity(.thinking(id, delta)) = events[0] else {
            Issue.record("expected replayed thinking")
            return
        }
        #expect(id == "t1")
        #expect(delta == "Checking")
        guard case let .activity(.toolCall(call)) = events[1] else {
            Issue.record("expected a replayed running tool")
            return
        }
        #expect(call.id == "call-1")
        #expect(call.tool == "search")
    }
}

// MARK: - Run state

/// The pure run-state facts the header and composer read. No view, no network.
struct ConversationRunStateTests {
    @Test func workingRunKeepsTheComposerBusy() {
        let state = ConversationRunState(bucket: "running", runID: "run-1", runStatus: "working")
        #expect(state.isWorking)
        #expect(state.isActive)
        #expect(state.isParked == false)
        #expect(state.headerStatus == .working)
    }

    @Test func parkedRunWaitsOnTheUser() {
        let state = ConversationRunState(
            bucket: "needs_input",
            runID: "run-1",
            runStatus: "working",
            pendingApprovals: 1,
            pendingQuestions: 2
        )
        // A parked run can still report `working`; the bucket is the authority.
        #expect(state.isWorking == false)
        #expect(state.isParked)
        #expect(state.pendingDecisionCount == 3)
        #expect(state.headerStatus == .waiting(3))
    }

    @Test func failedRunReadsAsRunFailed() {
        #expect(ConversationRunState(bucket: "failed", runID: "run-1", runStatus: "failed").headerStatus == .failed)
        #expect(ConversationRunState(bucket: "failed", runID: "run-1", runStatus: "error").headerStatus == .failed)
    }
}

// MARK: - Events model

/// A stream that ends immediately; the model tests drive `apply(_:)` directly.
private struct IdleConversationEventStream: ConversationEventStreaming {
    func events(conversationID _: String) -> AsyncThrowingStream<ChatStreamEvent, Error> {
        AsyncThrowingStream { continuation in continuation.finish() }
    }
}

@MainActor
struct ConversationEventsModelTests {
    private func makeModel() -> ConversationEventsModel {
        ConversationEventsModel(stream: IdleConversationEventStream())
    }

    @Test func workingRefreshShowsTheWorkingHeader() {
        let model = makeModel()
        model.apply(.refresh(ConversationRunState(bucket: "running", runID: "run-1", runStatus: "working")))
        #expect(model.state.isWorking)
        #expect(model.state.runID == "run-1")
        #expect(model.state.headerStatus == .working)
    }

    /// A parked run releases the composer while the header keeps saying who the
    /// run is waiting on (`emergent.memory#437`).
    @Test func parkedDecisionReleasesTheComposer() {
        let model = makeModel()
        model.apply(.refresh(ConversationRunState(bucket: "running", runID: "run-1", runStatus: "working")))
        model.apply(.refresh(ConversationRunState(
            bucket: "needs_input",
            runID: "run-1",
            runStatus: "working",
            pendingApprovals: 1,
            pendingQuestions: 1
        )))
        #expect(model.state.isWorking == false)
        #expect(model.state.runID == "run-1")
        #expect(model.state.headerStatus == .waiting(2))
    }

    @Test func completedRunReleasesTheComposer() {
        let model = makeModel()
        model.apply(.refresh(ConversationRunState(bucket: "running", runID: "run-1", runStatus: "working")))
        model.apply(.refresh(ConversationRunState(bucket: "done")))
        #expect(model.state.isWorking == false)
        #expect(model.state.runID == nil)
        #expect(model.state.headerStatus == nil)
    }

    /// A run absent from the server is the stale case: the next populated frame
    /// reports no run and must release the working state.
    @Test func staleRunClearsWorkingState() {
        let model = makeModel()
        model.apply(.refresh(ConversationRunState(bucket: "running", runID: "run-1", runStatus: "working")))
        model.apply(.refresh(ConversationRunState(bucket: "done", runStatus: nil)))
        #expect(model.state.isWorking == false)
        #expect(model.state.runID == nil)
        #expect(model.state.headerStatus == nil)
    }

    /// The fieldless connect frame carries no facts and must not clear the run
    /// this client is already showing.
    @Test func bareRefreshDoesNotClearACurrentRun() {
        let model = makeModel()
        model.apply(.refresh(ConversationRunState(bucket: "running", runID: "run-1", runStatus: "working")))
        model.apply(.refresh(ConversationRunState()))
        #expect(model.state.isWorking)
        #expect(model.state.runID == "run-1")
    }

    @Test func detachForgetsTheRunState() {
        let model = makeModel()
        model.apply(.refresh(ConversationRunState(bucket: "running", runID: "run-1", runStatus: "working")))
        model.detach()
        #expect(model.state.isWorking == false)
        #expect(model.state.runID == nil)
        #expect(model.state.headerStatus == nil)
    }
}

// MARK: - Transport integration

/// A 200 answer for the stub handler. File-level `nonisolated` so the
/// `@Sendable` handler does not capture the main-actor test suite.
private nonisolated func conversationEventsStubResponse(
    _ request: URLRequest,
    body: String
) -> (URLResponse, Data) {
    let response = HTTPURLResponse(
        url: request.url ?? URL(fileURLWithPath: "/"),
        statusCode: 200,
        httpVersion: nil,
        headerFields: nil
    )!
    return (response, Data(body.utf8))
}

/// Extends ``HTTPChatTransportTests`` so these run under its `.serialized`
/// trait: the stub protocol and its recorder are shared process-wide.
extension HTTPChatTransportTests {
    private func eventsTransport() -> HTTPChatTransport {
        HTTPChatTransport(
            config: MemoryConfig(),
            stream: InFlightChatStream(),
            session: makeSession()
        )
    }

    /// Pumps the main actor until `condition` holds, like the stop tests.
    private func waitFor(_ condition: () -> Bool) async {
        for _ in 0 ..< 1000 {
            if condition() {
                return
            }
            await Task.yield()
        }
    }

    /// A `refresh` frame carrying the active run id is the reattach handle: the
    /// transport must learn it from the events stream as well as from `meta`.
    @Test func refreshFrameSetsLiveRunID() {
        let transport = eventsTransport()
        let payload = #"{"type":"refresh","bucket":"running","runId":"run-live","runStatus":"working"}"#
        guard case let .refresh(state)? = ChatStreamFrameDecoder.decode(payload, fallbackToolID: { "fallback" }) else {
            Issue.record("expected a refresh frame")
            return
        }
        transport.applyRunRefresh(state)
        #expect(transport.liveRunID == "run-live")
        #expect(transport.isRunning)
        #expect(transport.isCancelling == false)
    }

    /// A working refresh is the stop-control gate even when this client is not
    /// streaming its own turn.
    @Test func anAttachedRunCanBeStoppedFromTheRefreshRunID() async {
        let recorder = RequestRecorder()
        HTTPChatStubProtocol.handler = { request in
            recorder.paths.append(request.url?.path ?? "")
            return conversationEventsStubResponse(request, body: #"{"ok":true}"#)
        }
        defer { HTTPChatStubProtocol.handler = nil }

        let transport = eventsTransport()
        transport.applyRunRefresh(ConversationRunState(bucket: "running", runID: "run-9", runStatus: "working"))

        let outcome = await transport.stop()

        #expect(outcome == .cancelled)
        #expect(recorder.paths == ["/api/chat/runs/run-9/cancel"])
        #expect(transport.isRunning == false)
        #expect(transport.isCancelling)
        #expect(transport.showsCancelledNotice)
    }

    /// A successful stop shows "Run cancelled." and holds the cancelling state
    /// until the next refresh settles it; a stopped turn is never interrupted.
    @Test func stopShowsCancelledAndHoldsUntilRefresh() async throws {
        HTTPChatStubProtocol.handler = { request in
            conversationEventsStubResponse(request, body: #"{"ok":true}"#)
        }
        defer { HTTPChatStubProtocol.handler = nil }

        let stream = InFlightChatStream()
        let transport = HTTPChatTransport(config: MemoryConfig(), stream: stream, session: makeSession())
        let task = try #require(transport.send(
            "hello",
            conversationID: nil,
            agentName: "",
            transcript: ChatSessionTranscriptStore(),
            activity: ChatActivityStore()
        ))
        await waitFor { stream.continuation != nil }
        stream.yield(ChatStreamFixtures.metaWithRun)
        await waitFor { transport.liveRunID == "run-1" }

        let outcome = await transport.stop()
        await task.value

        #expect(outcome == .cancelled)
        #expect(transport.isCancelling)
        #expect(transport.showsCancelledNotice)
        #expect(transport.isRunning == false)
        #expect(transport.interruptedTurn == nil)

        // The next refresh is the settled state and clears the held controls.
        transport.applyRunRefresh(ConversationRunState(bucket: "done"))
        #expect(transport.isCancelling == false)
        #expect(transport.showsCancelledNotice == false)
    }

    /// A run absent from the server releases a stale working state, including a
    /// run id that can no longer cancel anything.
    @Test func staleRunClearsWorkingState() {
        let transport = eventsTransport()
        transport.applyRunRefresh(ConversationRunState(bucket: "running", runID: "run-1", runStatus: "working"))
        #expect(transport.isRunning)
        #expect(transport.liveRunID == "run-1")

        transport.applyRunRefresh(ConversationRunState(bucket: "done"))

        #expect(transport.isRunning == false)
        #expect(transport.liveRunID == nil)
    }

    /// A completed run's refresh frame still names the run it finished. That id
    /// must not survive, or a stop before the next turn's `meta` frame would
    /// cancel the previous (already finished) run and leave the new one going.
    @Test func completedRunFrameClearsTheStaleRunID() async {
        let recorder = RequestRecorder()
        HTTPChatStubProtocol.handler = { request in
            recorder.paths.append(request.url?.path ?? "")
            return conversationEventsStubResponse(request, body: #"{"ok":true}"#)
        }
        defer { HTTPChatStubProtocol.handler = nil }

        let transport = eventsTransport()
        transport.applyRunRefresh(ConversationRunState(bucket: "running", runID: "run-A", runStatus: "working"))
        #expect(transport.liveRunID == "run-A")

        transport.applyRunRefresh(ConversationRunState(
            bucket: "done",
            runID: "run-A",
            runStatus: "completed"
        ))

        #expect(transport.liveRunID == nil)
        #expect(transport.isRunning == false)

        // With the stale id gone there is no run to cancel: stop() must not
        // POST run-A's cancel route.
        let outcome = await transport.stop()
        #expect(outcome == .noRunID)
        #expect(recorder.paths.isEmpty)
    }

    /// A new turn clears the attached run's id before its own `meta` arrives,
    /// so the same pre-meta stop cannot reach for the previous run.
    @Test func newTurnClearsTheAttachedRunID() async throws {
        let stream = InFlightChatStream()
        let transport = HTTPChatTransport(config: MemoryConfig(), stream: stream, session: makeSession())
        transport.applyRunRefresh(ConversationRunState(bucket: "running", runID: "run-A", runStatus: "working"))
        #expect(transport.liveRunID == "run-A")

        let task = try #require(transport.send(
            "next",
            conversationID: nil,
            agentName: "",
            transcript: ChatSessionTranscriptStore(),
            activity: ChatActivityStore()
        ))
        #expect(transport.liveRunID == nil)

        transport.reset()
        await task.value
    }

    /// The fieldless connect refresh must not clear the run this client streams
    /// (its `meta` frame is the only stop handle it has).
    @Test func bareRefreshKeepsTheStreamingRunID() async throws {
        let stream = InFlightChatStream()
        let transport = HTTPChatTransport(config: MemoryConfig(), stream: stream, session: makeSession())
        let task = try #require(transport.send(
            "hello",
            conversationID: nil,
            agentName: "",
            transcript: ChatSessionTranscriptStore(),
            activity: ChatActivityStore()
        ))
        await waitFor { stream.continuation != nil }
        stream.yield(ChatStreamFixtures.metaWithRun)
        await waitFor { transport.liveRunID == "run-1" }

        transport.applyRunRefresh(ConversationRunState())

        #expect(transport.liveRunID == "run-1")
        transport.reset()
        await task.value
    }
}
