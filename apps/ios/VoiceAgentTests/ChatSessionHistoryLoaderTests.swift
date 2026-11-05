import Foundation
@testable import Memory
import Testing

/// Serves one queued result per `detail` call, so a test can fail (or be
/// cancelled) once and then succeed — the retry path — and counts the calls.
///
/// Main-actor isolated like the app target's default isolation and the
/// ``SessionLogFetching`` seam it implements, so it is a class rather than an
/// actor.
@MainActor
final class SequencedSessionLogClient: SessionLogFetching {
    private var results: [Result<[SessionRecord], Error>]
    private(set) var detailRoomIDs: [String] = []

    init(_ results: [Result<[SessionRecord], Error>]) {
        self.results = results
    }

    var detailCallCount: Int {
        detailRoomIDs.count
    }

    func listSessions(agentID _: String?, includeArchived _: Bool) async throws -> [SessionSummary] {
        []
    }

    func detail(room: String) async throws -> SessionDetail {
        detailRoomIDs.append(room)
        // The last queued result repeats, so a test only queues what changes.
        let result = results.count > 1 ? results.removeFirst() : (results.first ?? .success([]))
        return try SessionDetail(room: room, records: result.get())
    }

    func archiveConversation(id _: String) async throws {}
    func unarchiveConversation(id _: String) async throws {}
    func deleteConversation(id _: String) async throws {}
    func respondQuestion(questionID _: String, response _: String, message _: String) async throws {}
}

/// The history loader's contract, and the bug it exists to prevent: a cancelled
/// fetch is not a failure, and it must not mark the conversation loaded (which
/// is what made the failure banner unrecoverable).
@MainActor
struct ChatSessionHistoryLoaderTests {
    private func record(_ text: String) -> SessionRecord {
        SessionRecord(kind: "turn", role: "user", text: text)
    }

    private func message(_ text: String) -> HistoryEntry {
        .message(ChatMessage(id: "history-0", role: .user, text: text))
    }

    @Test func cancelledLoadLeavesNoErrorAndTheNextLoadRetries() async {
        let fake = SequencedSessionLogClient([
            .failure(CancellationError()),
            .success([record("hello")]),
        ])
        let loader = ChatSessionHistoryLoader(client: fake)
        let transcript = ChatSessionTranscriptStore()

        let applied = await loader.load(conversationID: "conv-1", into: transcript)

        #expect(applied == false)
        #expect(loader.loadedConversationID == nil)
        #expect(transcript.historyError == nil)
        #expect(transcript.isLoadingHistory == false)
        #expect(transcript.history.isEmpty)

        // The view's task re-runs (or the user taps Retry): the conversation was
        // never marked loaded, so the fetch actually happens again.
        let retried = await loader.load(conversationID: "conv-1", into: transcript)

        #expect(retried)
        #expect(loader.loadedConversationID == "conv-1")
        #expect(transcript.historyError == nil)
        #expect(transcript.history == [message("hello")])
        #expect(fake.detailCallCount == 2)
    }

    @Test func realFailureSurfacesOnceAndDoesNotBlockTheNextLoad() async {
        let fake = SequencedSessionLogClient([
            .failure(GatewayError.transport("Session log unreachable")),
            .success([record("hello")]),
        ])
        let loader = ChatSessionHistoryLoader(client: fake)
        let transcript = ChatSessionTranscriptStore()

        let applied = await loader.load(conversationID: "conv-1", into: transcript)

        #expect(applied == false)
        #expect(loader.loadedConversationID == nil)
        #expect(transcript.historyError == "Session log unreachable")
        #expect(transcript.isLoadingHistory == false)

        let retried = await loader.load(conversationID: "conv-1", into: transcript)

        #expect(retried)
        #expect(transcript.historyError == nil)
        #expect(transcript.history == [message("hello")])
        #expect(fake.detailCallCount == 2)
    }

    @Test func loadedConversationIsNotRefetchedUnlessForced() async {
        let fake = SequencedSessionLogClient([.success([record("one")]), .success([record("two")])])
        let loader = ChatSessionHistoryLoader(client: fake)
        let transcript = ChatSessionTranscriptStore()

        let first = await loader.load(conversationID: "conv-1", into: transcript)
        let repeated = await loader.load(conversationID: "conv-1", into: transcript)

        #expect(first)
        #expect(repeated == false)
        #expect(fake.detailCallCount == 1)

        let forced = await loader.load(conversationID: "conv-1", force: true, into: transcript)

        #expect(forced)
        #expect(fake.detailCallCount == 2)
        #expect(transcript.history == [message("two")])
    }

    @Test func resetForcesTheNextLoadToRefetch() async {
        let fake = SequencedSessionLogClient([.success([record("one")])])
        let loader = ChatSessionHistoryLoader(client: fake)
        let transcript = ChatSessionTranscriptStore()

        _ = await loader.load(conversationID: "conv-1", into: transcript)
        loader.reset()

        #expect(loader.loadedConversationID == nil)

        let reloaded = await loader.load(conversationID: "conv-1", into: transcript)

        #expect(reloaded)
        #expect(fake.detailCallCount == 2)
    }

    @Test func cancellationClassificationCoversTaskAndURLSession() {
        #expect(Cancellation.isCancellation(CancellationError()))
        #expect(Cancellation.isCancellation(URLError(.cancelled)))
        #expect(!Cancellation.isCancellation(URLError(.notConnectedToInternet)))
        #expect(!Cancellation.isCancellation(GatewayError.transport("Session log unreachable")))
        // A stray cancellation must not print the stdlib boilerplate.
        #expect(CancellationError().localizedDescription == "The request was cancelled.")
    }
}
