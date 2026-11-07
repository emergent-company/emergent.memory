import Foundation

/// Waits, for a bounded window, for Memory to record the reply a dropped stream
/// cut off, then hands that detail to the caller.
///
/// Closing the stream does not cancel the run — Memory executes the agent on a
/// detached context — but it records the assistant reply only when the run ends,
/// which is after the last frame this client saw. Reloading at the drop
/// therefore returns the same truncated record, so the reply is waited for
/// instead.
///
/// The decision lives here, apart from the view, so it can be driven without a
/// network or a real twenty-second clock: a test supplies the fetch and the
/// sleep. Text mode cannot ask whether the run is still going (a device
/// credential has no run-status route, `emergent.memory#81`), which is why the
/// wait is bounded rather than open-ended.
@MainActor
struct InterruptedTurnRecovery {
    /// The session log the bounded window is spent reading.
    let client: any SessionLogFetching
    /// One-second record reads spent waiting for a finished reply.
    var attempts = 20
    var interval: Duration = .seconds(1)
    /// Injected in tests, where the waiting itself is not the subject.
    var sleep: (Duration) async -> Void = { try? await Task.sleep(for: $0) }

    /// Polls ``client`` until ``ChatSessionTranscriptStore/recordedReply(extending:in:since:)``
    /// accepts a detail, ``isCurrent`` turns false, or the window closes.
    ///
    /// `apply` runs exactly once, and only on a record that holds the finished
    /// reply. Giving up leaves the truncated live copy in place — the text the
    /// user already saw — rather than surfacing an error they cannot act on.
    func run(
        conversationID: String,
        partialText: String,
        baseline: Int,
        isCurrent: () -> Bool,
        apply: (SessionDetail) -> Void
    ) async {
        // The reading loop and its ownership checks are shared with the resumed
        // approval wait (see ``RecordedTurnPoll``); only the accept rule differs.
        let poll = RecordedTurnPoll(client: client, attempts: attempts, interval: interval, sleep: sleep)
        await poll.run(
            conversationID: conversationID,
            isCurrent: isCurrent,
            accept: { detail in
                ChatSessionTranscriptStore.recordedReply(
                    extending: partialText,
                    in: detail,
                    since: baseline
                )
            },
            apply: apply
        )
    }
}
