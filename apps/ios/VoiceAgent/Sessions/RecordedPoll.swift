import Foundation

/// What a bounded wait owns while it polls: the conversation it started on and
/// the record count it was waiting to grow.
///
/// A wait that no longer matches this must not write into the transcript. Two
/// things can end the ownership, and both used to be ignored:
/// - the user opened another chat, so the records belong to a conversation this
///   page no longer shows (`emergent.memory#208`);
/// - a reload replaced the transcript while the wait ran, so the baseline it was
///   measuring against no longer describes what is on screen, and any newer
///   record would pass for the one being waited on.
struct TranscriptWaitScope: Equatable {
    let conversationID: String
    let baseline: Int

    /// `openConversationID` is the view's current route/transport conversation
    /// and `recordCount` the transcript's loaded record count.
    func isCurrent(openConversationID: String?, recordCount: Int) -> Bool {
        openConversationID == conversationID && recordCount == baseline
    }
}

/// Polls one conversation's recorded transcript for a bounded window and hands
/// the first detail the caller accepts to `apply`.
///
/// The one engine behind both waits that cannot be told the answer: a dropped
/// stream (``InterruptedTurnRecovery``) and a card decision that resumed a run
/// in the background (``ResumedHistoryPoll``). Neither gets the continuation
/// streamed back, so both read until the record shows it.
///
/// `isCurrent` is checked *before and after* every read. Before, so a wait that
/// outlived its transcript stops without spending a request; after, because the
/// read itself takes time — a detail that lands after the user switched chats
/// belongs to the view the wait started in and is dropped rather than applied.
@MainActor
struct RecordedTurnPoll {
    /// The session log the window is spent reading.
    let client: any SessionLogFetching
    /// Record reads the window allows.
    var attempts: Int
    var interval: Duration = .seconds(1)
    /// Injected in tests, where the waiting itself is not the subject.
    var sleep: (Duration) async -> Void = { try? await Task.sleep(for: $0) }

    func run(
        conversationID: String,
        isCurrent: () -> Bool,
        accept: (SessionDetail) -> Bool,
        apply: (SessionDetail) -> Void
    ) async {
        for _ in 0 ..< attempts {
            await sleep(interval)
            guard !Task.isCancelled, isCurrent() else { return }
            guard let detail = try? await client.detail(room: conversationID) else { continue }
            guard isCurrent() else { return }
            guard accept(detail) else { continue }
            apply(detail)
            return
        }
    }
}

/// Waits for Memory to record the continuation of a run resumed by answering a
/// card, then hands that detail to the caller.
///
/// The gateway resumes the parked run in the background, so nothing streams back
/// over the live channel in text mode and the transcript has to be read until it
/// grows past the baseline. Answering a card and switching chats inside the
/// window used to write the old conversation's records into the new one; the
/// wait now stops as soon as the transcript is no longer the one it started for
/// (`emergent.memory#208`). Giving up leaves the transcript as it was — the user
/// can reopen the chat to load the record.
@MainActor
struct ResumedHistoryPoll {
    let client: any SessionLogFetching
    /// Record reads the window allows; the continuation is usually one run.
    var attempts = 30
    var interval: Duration = .seconds(1)
    var sleep: (Duration) async -> Void = { try? await Task.sleep(for: $0) }

    func run(
        scope: TranscriptWaitScope,
        isCurrent: () -> Bool,
        apply: (SessionDetail) -> Void
    ) async {
        let poll = RecordedTurnPoll(client: client, attempts: attempts, interval: interval, sleep: sleep)
        await poll.run(
            conversationID: scope.conversationID,
            isCurrent: isCurrent,
            // Growth past the scope's own baseline is the signal: the resumed
            // run appends at least one record when it finishes.
            accept: { $0.records.count > scope.baseline },
            apply: apply
        )
    }
}
