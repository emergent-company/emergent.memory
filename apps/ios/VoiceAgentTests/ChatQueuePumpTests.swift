import Foundation
@testable import Memory
import Testing

// MARK: - Turn end → queue pump

// The transport's side of the composer queue (`emergent.memory#438`): a turn
// that ends on its own tells the view, with the streaming flag already clear so
// the pump may start the next turn. A stopped turn stays silent — the user ended
// it on purpose, and its parked rows wait for an explicit action.

extension HTTPChatTransportTests {
    /// Answers the best-effort agent lookup with a miss.
    private func stubMissingAgent() {
        HTTPChatStubProtocol.handler = { request in
            let response = HTTPURLResponse(
                url: request.url ?? URL(fileURLWithPath: "/"),
                statusCode: 404,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data())
        }
    }

    @Test func finishedTurnNotifiesTheQueue() async throws {
        stubMissingAgent()
        defer { HTTPChatStubProtocol.handler = nil }
        let transport = HTTPChatTransport(
            config: MemoryConfig(),
            stream: StubChatStream(payloads: [ChatStreamFixtures.meta, ChatStreamFixtures.tokenA, ChatStreamFixtures.done]),
            session: makeSession()
        )
        var finished = 0
        var streamingWhenNotified: Bool?
        transport.onTurnFinished = {
            streamingWhenNotified = transport.isStreaming
            finished += 1
        }

        let task = try #require(transport.send(
            "hello",
            conversationID: nil,
            agentName: "alfred",
            transcript: ChatSessionTranscriptStore(),
            activity: ChatActivityStore()
        ))
        await task.value

        #expect(finished == 1)
        // The pump runs after the turn's own teardown: it must see an idle
        // transport or it would refuse to release the next row.
        #expect(streamingWhenNotified == false)
        #expect(!transport.isStreaming)
    }

    /// A stopped turn must not release the queue: the user ended that turn, so
    /// the rows parked behind it stay parked.
    @Test func stoppedTurnDoesNotNotifyTheQueue() async throws {
        HTTPChatStubProtocol.handler = { request in
            let response = HTTPURLResponse(
                url: request.url ?? URL(fileURLWithPath: "/"),
                statusCode: 200,
                httpVersion: nil,
                headerFields: nil
            )!
            return (response, Data(#"{"ok":true}"#.utf8))
        }
        defer { HTTPChatStubProtocol.handler = nil }
        let stream = InFlightChatStream()
        let transport = HTTPChatTransport(config: MemoryConfig(), stream: stream, session: makeSession())
        var finished = 0
        transport.onTurnFinished = { finished += 1 }

        let task = try #require(transport.send(
            "hello",
            conversationID: nil,
            agentName: "",
            transcript: ChatSessionTranscriptStore(),
            activity: ChatActivityStore()
        ))
        await waitUntil { stream.continuation != nil }
        stream.yield(ChatStreamFixtures.metaWithRun)
        await waitUntil { transport.liveRunID == "run-1" }

        await transport.stop()
        await task.value

        #expect(finished == 0)
        #expect(!transport.isStreaming)
    }

    /// A reset (the view left the chat) is an abort too.
    @Test func resetTurnDoesNotNotifyTheQueue() async throws {
        stubMissingAgent()
        defer { HTTPChatStubProtocol.handler = nil }
        let stream = InFlightChatStream()
        let transport = HTTPChatTransport(config: MemoryConfig(), stream: stream, session: makeSession())
        var finished = 0
        transport.onTurnFinished = { finished += 1 }

        let task = try #require(transport.send(
            "hello",
            conversationID: nil,
            agentName: "alfred",
            transcript: ChatSessionTranscriptStore(),
            activity: ChatActivityStore()
        ))
        await waitUntil { stream.continuation != nil }

        transport.reset()
        await task.value

        #expect(finished == 0)
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
}
