import Foundation
@testable import Memory
import Testing

// MARK: - Stopping a text turn

// Stopping a turn (`emergent.memory#436`): the run id the stream's `meta` frame
// names, the frozen cancel route, and the rule that a local abort alone never
// counts as a cancellation.
//
// An extension of `HTTPChatTransportTests` in its own file: staying one suite
// keeps these tests under its `.serialized` trait, so they share its
// `HTTPChatStubProtocol` handler without clobbering it, and the stop tests do not
// push that suite's file and body past the lint limits.

/// A `ChatStreaming` stub the test feeds frame by frame, so a turn can be held in
/// flight while something (a stop) acts on it.
///
/// Cancelling the consuming task ends the iteration exactly like the live
/// client's `URLSession.bytes` loop does, which is what lets `stop()` settle the
/// turn before it cancels the run.
final class InFlightChatStream: ChatStreaming, @unchecked Sendable {
    nonisolated(unsafe) var continuation: AsyncThrowingStream<String, Error>.Continuation?

    func frames(_: ChatTurnRequest) -> AsyncThrowingStream<String, Error> {
        AsyncThrowingStream { continuation in
            self.continuation = continuation
        }
    }

    func yield(_ payload: String) {
        continuation?.yield(payload)
    }

    func finish() {
        continuation?.finish()
        continuation = nil
    }
}

/// An HTTP response for the stub handler, so a test states only the body it
/// answers with.
private nonisolated func stopStubResponse(for request: URLRequest) -> HTTPURLResponse {
    HTTPURLResponse(
        url: request.url ?? URL(fileURLWithPath: "/"),
        statusCode: 200,
        httpVersion: nil,
        headerFields: nil
    )!
}

extension HTTPChatTransportTests {
    /// A transport over the stub session and a stream the test drives, so a turn
    /// can be held in flight.
    private func makeTransport(stream: InFlightChatStream) -> HTTPChatTransport {
        HTTPChatTransport(config: MemoryConfig(), stream: stream, session: makeSession())
    }

    /// Sends a turn and waits until its stream is open. `agentName` is empty, so
    /// the only request a test can observe is the one it stubs.
    private func startHeldTurn(
        _ transport: HTTPChatTransport,
        _ stream: InFlightChatStream
    ) async throws -> Task<Void, Never> {
        let task = try #require(transport.send(
            "hello",
            conversationID: nil,
            agentName: "",
            transcript: ChatSessionTranscriptStore(),
            activity: ChatActivityStore()
        ))
        await waitUntil { stream.continuation != nil }
        return task
    }

    /// Pumps the main actor until `condition` holds. No wall-clock sleep: each
    /// yield lets the turn task consume the next frame.
    private func waitUntil(_ condition: () -> Bool) async {
        for _ in 0 ..< 1000 {
            if condition() {
                return
            }
            await Task.yield()
        }
    }

    /// A `meta` frame's run id is the handle a stop cancels; the transport used
    /// to discard it. It must not outlive the turn either.
    @Test func metaFrameKeepsTheLiveRunID() async throws {
        let stream = InFlightChatStream()
        let transport = makeTransport(stream: stream)
        let task = try await startHeldTurn(transport, stream)

        stream.yield(ChatStreamFixtures.metaWithRun)
        await waitUntil { transport.liveRunID != nil }
        #expect(transport.liveRunID == "run-1")

        stream.yield(ChatStreamFixtures.done)
        stream.finish()
        await task.value
        #expect(transport.liveRunID == nil)
        #expect(transport.isStreaming == false)
    }

    /// `stop()` posts the frozen cancel route with an empty object body — the
    /// web's `requestRunCancel` shape.
    @Test func stopPostsTheRunCancelRoute() async throws {
        let recorder = RequestRecorder()
        HTTPChatStubProtocol.handler = { request in
            recorder.paths.append(request.url?.path ?? "")
            recorder.methods.append(request.httpMethod ?? "")
            recorder.bodies.append(chatRequestBody(request))
            return (stopStubResponse(for: request), Data(#"{"ok":true}"#.utf8))
        }
        defer { HTTPChatStubProtocol.handler = nil }
        let stream = InFlightChatStream()
        let transport = makeTransport(stream: stream)
        let task = try await startHeldTurn(transport, stream)
        stream.yield(ChatStreamFixtures.metaWithRun)
        await waitUntil { transport.liveRunID == "run-1" }

        let outcome = await transport.stop()
        await task.value

        #expect(outcome == .cancelled)
        #expect(recorder.paths == ["/api/chat/runs/run-1/cancel"])
        #expect(recorder.methods == ["POST"])
        let body = try #require(recorder.bodies.first ?? nil)
        #expect(String(data: body, encoding: .utf8) == "{}")
        // A stopped turn is not an interrupted one: no banner and no recovery.
        #expect(transport.errorMessage == nil)
        #expect(transport.interruptedTurn == nil)
        #expect(transport.isStreaming == false)
        #expect(transport.liveRunID == nil)
    }

    /// `{"ok": true}` is the only success; a refusal surfaces its reason.
    @Test func stopRefusedSurfacesTheReason() async throws {
        HTTPChatStubProtocol.handler = { request in
            let response = stopStubResponse(for: request)
            return (response, Data(#"{"ok":false,"reason":"run already finished"}"#.utf8))
        }
        defer { HTTPChatStubProtocol.handler = nil }
        let stream = InFlightChatStream()
        let transport = makeTransport(stream: stream)
        let task = try await startHeldTurn(transport, stream)
        stream.yield(ChatStreamFixtures.metaWithRun)
        await waitUntil { transport.liveRunID == "run-1" }

        let outcome = await transport.stop()
        await task.value

        #expect(outcome == .failed("run already finished"))
        #expect(transport.errorMessage == "Could not cancel the server run: run already finished")
    }

    /// Without a run id nothing is claimed and no request is made: a local abort
    /// is not a cancellation.
    @Test func stopWithoutARunIDSendsNothing() async throws {
        let recorder = RequestRecorder()
        HTTPChatStubProtocol.handler = { request in
            recorder.paths.append(request.url?.path ?? "")
            return (stopStubResponse(for: request), Data())
        }
        defer { HTTPChatStubProtocol.handler = nil }
        let stream = InFlightChatStream()
        let transport = makeTransport(stream: stream)
        let task = try await startHeldTurn(transport, stream)
        // A conversation without a run id: the server has not named one.
        stream.yield(ChatStreamFixtures.metaWithoutConversation)

        let outcome = await transport.stop()
        await task.value

        #expect(outcome == .noRunID)
        #expect(recorder.paths.isEmpty)
        #expect(transport.isStreaming == false)
        #expect(transport.errorMessage?.contains("no active run id") == true)
    }
}
