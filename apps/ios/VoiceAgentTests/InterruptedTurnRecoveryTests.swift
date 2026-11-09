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

    /// The gateway records a function-call-only step as an assistant turn of its
    /// own — `"(calls: …)"`, `messageRecord` in `apps/web-ui/gateway/client_api.go`
    /// — so a reply that opened with a tool call is stored as the placeholder and
    /// *then* the answer. The text the client saw starts after the placeholder,
    /// and the wait has to find it there (#213 review).
    @Test func recognisesAReplyBehindTheGatewaysToolPlaceholder() {
        let placeholder = turn("assistant", "(calls: entity-create)")
        let detail = SessionDetail(room: "conv-1", records: [
            turn("user", "hello"),
            placeholder,
            SessionRecord(kind: "tools_executed", calls: [], outputs: []),
            turn("assistant", "hi there"),
        ])

        #expect(ChatSessionTranscriptStore.recordedReply(extending: "hi the", in: detail, since: 2))

        // The placeholder on its own is not the answer.
        let parked = SessionDetail(room: "conv-1", records: [
            turn("user", "hello"),
            placeholder,
            SessionRecord(kind: "tools_executed", calls: [], outputs: []),
        ])
        #expect(!ChatSessionTranscriptStore.recordedReply(extending: "hi the", in: parked, since: 2))
    }

    /// A placeholder-only suffix is not progress. The gateway records its
    /// function-call step as an assistant turn of its own, and the client never
    /// streams that text, so a record holding `"hi the"` and then
    /// `"(calls: search)"` has not finished the reply. Judging the placeholder
    /// as reply text settled the wait early and dropped the answer (#231).
    @Test func aToolPlaceholderDoesNotSettleTheTruncatedCopy() {
        let detail = SessionDetail(room: "conv-1", records: [
            turn("user", "hello"),
            turn("assistant", "hi the"),
            turn("assistant", "(calls: search)"),
        ])

        #expect(!ChatSessionTranscriptStore.recordedReply(extending: "hi the", in: detail, since: 2))
    }

    /// The partial can hold answer text written *after* a tool call:
    /// `"Let me check. Hi"`. The gateway's placeholder sits between the two
    /// halves of the reply; ignoring it joins them into the text the client
    /// saw, so the recorded continuation is swapped in instead of the wait
    /// running out its window (#231).
    @Test func recognisesAReplyWhosePartialContinuesPastAToolPlaceholder() {
        let detail = SessionDetail(room: "conv-1", records: [
            turn("user", "hello"),
            turn("assistant", "Let me check."),
            turn("assistant", "(calls: search)"),
            SessionRecord(kind: "tools_executed", calls: [], outputs: []),
            turn("assistant", " Hi there"),
        ])

        #expect(ChatSessionTranscriptStore.recordedReply(extending: "Let me check. Hi", in: detail, since: 2))
    }

    /// Only the gateway's exact shape is a placeholder. Answer text that merely
    /// opens with the same words is still the reply, so the shape is matched
    /// in full rather than by prefix (#231).
    @Test func answerTextThatOnlyOpensLikeAPlaceholderIsStillTheReply() {
        let detail = SessionDetail(room: "conv-1", records: [
            turn("user", "hello"),
            turn("assistant", "(calls: search) and then"),
        ])

        #expect(ChatSessionTranscriptStore.recordedReply(extending: "(calls: search)", in: detail, since: 1))
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

    // MARK: - A turn the client saw nothing of (#445)

    /// The gateway records the user's own message when it accepts the turn,
    /// before the run starts, so the first read after a dropped stream holds
    /// growth that is not a reply. Accepting it would settle the wait with an
    /// empty turn on screen.
    @Test func growthHoldingOnlyTheUsersOwnMessageIsNotAReply() {
        let detail = SessionDetail(room: "conv-1", records: [turn("user", "hello")])

        #expect(!ChatSessionTranscriptStore.recordedUnseenReply(in: detail, since: 0))
    }

    /// Once the run is recorded, the same read holds the answer.
    @Test func recognisesTheRecordedReplyOfATurnNothingStreamed() {
        let detail = SessionDetail(room: "conv-1", records: finished())

        #expect(ChatSessionTranscriptStore.recordedUnseenReply(in: detail, since: 1))
    }

    /// A tool step on its own is not an answer: the turn is still working, and
    /// the text may follow.
    @Test func aToolStepWithoutAnAnswerKeepsWaiting() {
        let detail = SessionDetail(room: "conv-1", records: [
            turn("user", "hello"),
            SessionRecord(kind: "tools_executed", calls: [], outputs: []),
        ])

        #expect(!ChatSessionTranscriptStore.recordedUnseenReply(in: detail, since: 0))
    }

    /// A reply behind the gateway's function-call placeholder is still the reply.
    @Test func recognisesAnAnswerBehindAToolCall() {
        let detail = SessionDetail(room: "conv-1", records: [
            turn("user", "hello"),
            turn("assistant", "(calls: entity-create)"),
            SessionRecord(kind: "tools_executed", calls: [], outputs: []),
            turn("assistant", "here you go"),
        ])

        #expect(ChatSessionTranscriptStore.recordedUnseenReply(in: detail, since: 0))
    }

    /// Without growth past the baseline the record is the one already on screen.
    @Test func anUnchangedRecordIsNotAReply() {
        let detail = SessionDetail(room: "conv-1", records: finished())

        #expect(!ChatSessionTranscriptStore.recordedUnseenReply(in: detail, since: 3))
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
