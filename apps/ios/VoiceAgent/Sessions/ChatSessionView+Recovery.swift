import Foundation

/// Recovering a reply whose stream died mid-turn.
///
/// Closing the stream does not cancel the run — Memory executes the agent on a
/// detached context — but it records the assistant reply only when the run ends,
/// which is after the last frame this client saw. Reloading at the drop
/// therefore returns the same truncated record, so the reply is waited for
/// instead: the record is read until it holds the finished text, and the
/// truncated live copy is dropped in the same step so the turn cannot render
/// twice. The wait itself is ``InterruptedTurnRecovery``.
///
/// Text mode cannot ask whether the run is still going (a device credential has
/// no run-status route, `emergent.memory#81`), so this polls for a bounded
/// window. The limits, stated rather than implied:
/// - it is one wait per drop and is not cancelled if the user sends another
///   message meanwhile — that turn's own live copy is unaffected because only
///   the interrupted message id is dropped;
/// - the wait stops as soon as the transcript is no longer the one it was
///   started for (another chat was opened, or its records were replaced), so a
///   slow read cannot land in a view that moved on;
/// - a run that never records a longer reply leaves the truncated copy in place
///   (the text the user already saw) rather than surfacing an error: there is
///   nothing actionable to tell them, and reopening the chat loads the record.
extension ChatSessionView {
    func recoverInterruptedTurn() async {
        guard
            let interrupted = transport.interruptedTurn,
            let conversationID = openConversationID
        else { return }
        let baseline = transcript.lastRecordCount
        let recovery = InterruptedTurnRecovery(client: SessionLogClient(config: MemoryConfig()))
        await recovery.run(
            conversationID: conversationID,
            partialText: interrupted.partialText,
            baseline: baseline,
            isCurrent: {
                transport.interruptedTurn?.liveMessageID == interrupted.liveMessageID
                    && openConversationID == conversationID
                    && transcript.lastRecordCount == baseline
            },
            apply: { detail in
                transcript.loadHistory(from: detail)
                // Also clears the drop's banner: the record now supersedes the
                // partial, so the failure message would be the only thing still
                // claiming the turn was cut short. A drop that never recovers
                // keeps it — that is the signal the reply was lost.
                transport.discardInterruptedLiveCopy(in: transcript)
            }
        )
    }
}
