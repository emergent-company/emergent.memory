import Foundation
@testable import Memory
import Testing

/// The bounded wait for a recorded reply: when it swaps, when it gives up, and
/// when it refuses to write into a transcript that moved on.
@MainActor
struct InterruptedTurnRecoveryTests {
    private func turn(_ role: String, _ text: String) -> SessionRecord {
        SessionRecord(kind: "turn", role: role, text: text)
    }

    /// The state at the drop: the user's turn and the part of the reply that
    /// reached the screen.
    private func truncated() -> [SessionRecord] {
        [turn("user", "hello"), turn("assistant", "hi the")]
    }

    private func finished() -> [SessionRecord] {
        [turn("user", "hello"), turn("assistant", "hi there")]
    }

    private func recovery(
        _ client: SequencedSessionLogClient,
        attempts: Int = 20
    ) -> InterruptedTurnRecovery {
        // The wait itself is not what these tests are about, so it is skipped.
        InterruptedTurnRecovery(client: client, attempts: attempts, sleep: { _ in })
    }

    @Test func swapsOnceWhenTheRecordHoldsTheFinishedReply() async {
        let client = SequencedSessionLogClient([.success(truncated()), .success(finished())])
        var applied: [SessionDetail] = []

        await recovery(client).run(
            conversationID: "conv-1",
            partialText: "hi the",
            baseline: 1,
            isCurrent: { true },
            apply: { applied.append($0) }
        )

        #expect(applied.count == 1)
        #expect(applied.first?.records.last?.text == "hi there")
        // Two reads: the truncated one it kept waiting through, then the record.
        #expect(client.detailCallCount == 2)
    }

    @Test func givesUpLeavingTheTruncatedCopyAlone() async {
        let client = SequencedSessionLogClient([.success(truncated())])
        var applied = 0

        await recovery(client, attempts: 3).run(
            conversationID: "conv-1",
            partialText: "hi the",
            baseline: 1,
            isCurrent: { true },
            apply: { _ in applied += 1 }
        )

        #expect(applied == 0)
        #expect(client.detailCallCount == 3)
    }

    /// A failed read is not the end of the wait: the run is still going, and the
    /// next read can be the one that finds the reply.
    @Test func aFailedReadDoesNotEndTheWait() async {
        let client = SequencedSessionLogClient([
            .failure(URLError(.networkConnectionLost)),
            .success(truncated()),
            .success(finished()),
        ])
        var applied = 0

        await recovery(client).run(
            conversationID: "conv-1",
            partialText: "hi the",
            baseline: 1,
            isCurrent: { true },
            apply: { _ in applied += 1 }
        )

        #expect(applied == 1)
        #expect(client.detailCallCount == 3)
    }

    @Test func stopsWithoutReadingWhenTheTranscriptAlreadyMovedOn() async {
        let client = SequencedSessionLogClient([.success(finished())])
        var applied = 0

        await recovery(client).run(
            conversationID: "conv-1",
            partialText: "hi the",
            baseline: 1,
            isCurrent: { false },
            apply: { _ in applied += 1 }
        )

        #expect(applied == 0)
        #expect(client.detailCallCount == 0)
    }

    /// The read can land after the chat was switched: that detail belongs to the
    /// view the wait started in, so it is dropped rather than applied.
    @Test func discardsAReadThatLandsAfterTheTranscriptMovedOn() async {
        let client = SequencedSessionLogClient([.success(finished())])
        let checks = CheckCounter(limit: 1)
        var applied = 0

        await recovery(client).run(
            conversationID: "conv-1",
            partialText: "hi the",
            baseline: 1,
            isCurrent: { checks.next() },
            apply: { _ in applied += 1 }
        )

        #expect(applied == 0)
        #expect(client.detailCallCount == 1)
    }

    /// A resumed run appends a record holding only the tail of the reply. The
    /// newest raw record does not start with what the client saw, but the merged
    /// entry the transcript renders does.
    @Test func recognisesAMergedReplyWhoseNewestRecordIsOnlyTheTail() {
        let records = [
            turn("user", "hello"),
            turn("assistant", "hi there"),
            turn("assistant", " again"),
        ]
        let detail = SessionDetail(room: "conv-1", records: records)

        #expect(ChatSessionTranscriptStore.recordedReply(extending: "hi there", in: detail, since: 2))
    }

    /// A reply that called a tool between two assistant segments renders as two
    /// entries, so the second one does not start with what the client saw. The
    /// reply itself does, and that is what the wait recognises (#213).
    @Test func recognisesAReplyASplitToolCallInterrupts() {
        let records = [
            turn("user", "hello"),
            turn("assistant", "hi the"),
            SessionRecord(kind: "tools_executed", calls: [], outputs: []),
            turn("assistant", "re"),
        ]
        let detail = SessionDetail(room: "conv-1", records: records)

        #expect(ChatSessionTranscriptStore.recordedReply(extending: "hi the", in: detail, since: 2))
    }

    /// The tool record is not itself the reply: with only the truncated copy
    /// recorded, a tool call in between must not look like progress.
    @Test func aToolCallDoesNotMakeTheTruncatedCopyLookFinished() {
        let records = [
            turn("user", "hello"),
            turn("assistant", "hi the"),
            SessionRecord(kind: "tools_executed", calls: [], outputs: []),
        ]
        let detail = SessionDetail(room: "conv-1", records: records)

        #expect(!ChatSessionTranscriptStore.recordedReply(extending: "hi the", in: detail, since: 2))
    }
}

/// Answers `true` for the first `limit` checks, then `false`.
@MainActor
private final class CheckCounter {
    private let limit: Int
    private var count = 0

    init(limit: Int) {
        self.limit = limit
    }

    func next() -> Bool {
        count += 1
        return count <= limit
    }
}
