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
    /// Refreshes the rendered timeline from the record.
    ///
    /// The recovery and resumed-run paths apply a `SessionDetail` to the
    /// transcript directly, because that is the record-growth signal they poll.
    /// The rows on screen come from the ordered history endpoint, so that is
    /// refetched here; a reload replaces the run's rows by id/run id, so the
    /// partial (or the live copy) is superseded rather than stacked.
    func refreshTimelineFromRecord(conversationID: String) {
        Task { @MainActor in
            await history.load(
                conversationID: conversationID,
                force: true,
                into: transcript,
                timeline: timeline
            )
        }
    }

    func recoverInterruptedTurn() async {
        guard
            let interrupted = transport.interruptedTurn,
            let conversationID = openConversationID
        else { return }
        let baseline = transcript.lastRecordCount
        let scope = TranscriptWaitScope(conversationID: conversationID, baseline: baseline)
        let recovery = InterruptedTurnRecovery(client: SessionLogClient(config: MemoryConfig()))
        await recovery.run(
            conversationID: conversationID,
            partialText: interrupted.partialText,
            baseline: baseline,
            isCurrent: {
                transport.interruptedTurn?.liveMessageID == interrupted.liveMessageID
                    && scopeIsCurrent(scope)
            },
            apply: { detail in
                transcript.loadHistory(from: detail)
                refreshTimelineFromRecord(conversationID: conversationID)
                // Also clears this drop's banner: the record now supersedes the
                // partial, so the failure message would be the only thing still
                // claiming the turn was cut short. Only this drop's — a later
                // turn may have failed over the same slot meanwhile (#213). A
                // drop that never recovers keeps it: that is the signal the
                // reply was lost.
                transport.discardInterruptedLiveCopy(in: transcript)
            }
        )
    }

    /// Recovers a turn whose stream never delivered a token
    /// (`emergent.memory#445`).
    ///
    /// Nothing of the turn is on screen — not even a partial — so there is no
    /// copy to keep and no text to recognise the reply by. Growth past the
    /// pre-turn count is not a signal on its own: the gateway records the user's
    /// own message before the run starts, so the first read can hold nothing but
    /// that message. The wait accepts a detail only once it carries an assistant
    /// turn after the last user turn
    /// (``ChatSessionTranscriptStore/recordedUnseenReply(in:since:)``), and then
    /// both halves of the turn come from the record.
    ///
    /// A run can outlive the window; then the banner stays as the honest signal
    /// and reopening the chat loads the record (`emergent.memory#437` is the
    /// reattach that removes the wait altogether).
    func recoverUnansweredTurn() async {
        guard
            let unanswered = transport.unansweredTurn,
            let conversationID = openConversationID
        else { return }
        let baseline = transcript.lastRecordCount
        let scope = TranscriptWaitScope(conversationID: conversationID, baseline: baseline)
        let poll = RecordedTurnPoll(client: SessionLogClient(config: MemoryConfig()), attempts: 30)
        await poll.run(
            conversationID: conversationID,
            isCurrent: {
                transport.unansweredTurn?.id == unanswered.id && scopeIsCurrent(scope)
            },
            accept: { ChatSessionTranscriptStore.recordedUnseenReply(in: $0, since: baseline) },
            apply: { detail in
                transcript.loadHistory(from: detail)
                refreshTimelineFromRecord(conversationID: conversationID)
                // Also drops the live copy of this turn and clears this turn's
                // banner: the record now holds both halves (#213: only this
                // turn's — a later one may have failed over the same slot).
                transport.settleUnansweredTurn(in: transcript, activity: activity)
            }
        )
    }
}
