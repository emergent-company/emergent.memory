import Foundation
@testable import Memory
import Testing

/// The wait for a card decision's resumed run: it applies the continuation once
/// the record grows, and refuses to write into a transcript that moved on.
@MainActor
struct RecordedPollTests {
    private func turn(_ role: String, _ text: String) -> SessionRecord {
        SessionRecord(kind: "turn", role: role, text: text)
    }

    /// The conversation before the decision: one answered card's exchange.
    private func parked() -> [SessionRecord] {
        [turn("user", "deploy it"), turn("assistant", "Approve the deploy?")]
    }

    /// The same conversation after the resumed run appended its continuation.
    private func resumed() -> [SessionRecord] {
        [turn("user", "deploy it"), turn("assistant", "Approve the deploy?"), turn("assistant", "Deployed.")]
    }

    private func poll(
        _ client: SequencedSessionLogClient,
        attempts: Int = 30
    ) -> ResumedHistoryPoll {
        // The waiting is not what these tests are about, so it is skipped.
        ResumedHistoryPoll(client: client, attempts: attempts, sleep: { _ in })
    }

    // MARK: - The scope rule

    /// The rule the view builds every wait's ownership check from: the same chat
    /// is open, and it still holds the records the wait started against.
    @Test func scopeIsCurrentOnlyForTheSameConversationAndBaseline() {
        let scope = TranscriptWaitScope(conversationID: "conv-1", baseline: 2)

        #expect(scope.isCurrent(openConversationID: "conv-1", recordCount: 2))
        // Another chat was opened: its records are not this wait's to write.
        #expect(!scope.isCurrent(openConversationID: "conv-2", recordCount: 2))
        #expect(!scope.isCurrent(openConversationID: nil, recordCount: 2))
        // A reload replaced the transcript: the baseline no longer describes it.
        #expect(!scope.isCurrent(openConversationID: "conv-1", recordCount: 4))
    }

    // MARK: - The wait

    @Test func appliesTheContinuationOnceTheRecordGrows() async {
        let client = SequencedSessionLogClient([.success(parked()), .success(resumed())])
        var applied: [SessionDetail] = []

        await poll(client).run(
            scope: TranscriptWaitScope(conversationID: "conv-1", baseline: 2),
            isCurrent: { true },
            apply: { applied.append($0) }
        )

        #expect(applied.count == 1)
        #expect(applied.first?.records.last?.text == "Deployed.")
        // Two reads: the parked detail it kept waiting through, then the record.
        #expect(client.detailCallCount == 2)
        // The read is the decision's conversation, not whatever is open now.
        #expect(client.detailRoomIDs == ["conv-1", "conv-1"])
    }

    /// A detail that has not grown is not the continuation: a resumed run can be
    /// slow, and the window keeps reading until it records.
    @Test func keepsWaitingWhenTheRecordHasNotGrown() async {
        let client = SequencedSessionLogClient([.success(parked())])
        var applied = 0

        await poll(client, attempts: 3).run(
            scope: TranscriptWaitScope(conversationID: "conv-1", baseline: 2),
            isCurrent: { true },
            apply: { _ in applied += 1 }
        )

        #expect(applied == 0)
        #expect(client.detailCallCount == 3)
    }

    /// Answering a card and switching chats inside the window: the wait stops
    /// without spending a request, and the new chat keeps its own transcript.
    @Test func stopsWithoutReadingWhenTheChatWasSwitched() async {
        let client = SequencedSessionLogClient([.success(resumed())])
        var applied = 0

        await poll(client).run(
            scope: TranscriptWaitScope(conversationID: "conv-1", baseline: 2),
            isCurrent: { false },
            apply: { _ in applied += 1 }
        )

        #expect(applied == 0)
        #expect(client.detailCallCount == 0)
    }

    /// The switch can happen while the read is in flight: that detail belongs to
    /// the view the wait started in, so it is dropped rather than applied.
    @Test func discardsAReadThatLandsAfterTheChatWasSwitched() async {
        let client = SequencedSessionLogClient([.success(resumed())])
        let checks = OwnershipChecks(limit: 1)
        var applied = 0

        await poll(client).run(
            scope: TranscriptWaitScope(conversationID: "conv-1", baseline: 2),
            isCurrent: { checks.next() },
            apply: { _ in applied += 1 }
        )

        #expect(applied == 0)
        #expect(client.detailCallCount == 1)
    }

    /// A reload replaced the transcript mid-window: records past the old
    /// baseline no longer prove the resumed run finished. The rule is the same
    /// ownership check (see ``scopeIsCurrentOnlyForTheSameConversationAndBaseline``),
    /// so the wait stops exactly as it does for a switched chat.
    @Test func stopsWhenTheTranscriptWasReplacedByAReload() async {
        let client = SequencedSessionLogClient([.success(resumed())])
        var applied = 0
        // The view now holds four records for the same chat; the wait started at
        // two, so its baseline no longer describes what is on screen.
        let current = TranscriptWaitScope(conversationID: "conv-1", baseline: 2)
        let replaced = 4

        await poll(client).run(
            scope: current,
            isCurrent: { current.isCurrent(openConversationID: "conv-1", recordCount: replaced) },
            apply: { _ in applied += 1 }
        )

        #expect(applied == 0)
        #expect(client.detailCallCount == 0)
    }
}

/// Answers `true` for the first `limit` ownership checks, then `false` — the
/// switch lands between the pre-read and post-read check.
@MainActor
private final class OwnershipChecks {
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
