import Foundation

/// Recovering a reply whose stream died mid-turn.
///
/// Closing the stream does not cancel the run — Memory executes the agent on a
/// detached context — but it records the assistant reply only when the run ends,
/// which is after the last frame this client saw. Reloading at the drop
/// therefore returns the same truncated record, so the reply is waited for
/// instead: the record is read until it holds the finished text, and the
/// truncated live copy is dropped in the same step so the turn cannot render
/// twice.
///
/// Text mode cannot ask whether the run is still going (a device credential has
/// no run-status route, `emergent.memory#81`), so this polls for a bounded
/// window. The limits, stated rather than implied:
/// - it is one poll per drop and is not cancelled if the user sends another
///   message meanwhile — that turn's own live copy is unaffected because only
///   the interrupted message id is dropped;
/// - a run that never records a longer reply leaves the truncated copy in place
///   (the text the user already saw) rather than surfacing an error: there is
///   nothing actionable to tell them, and reopening the chat loads the record.
extension ChatSessionView {
    /// One-second record reads spent waiting for a finished reply.
    private static let recoveryAttempts = 20

    func recoverInterruptedTurn() async {
        guard
            let interrupted = transport.interruptedTurn,
            let conversationID = openConversationID
        else { return }
        let baseline = transcript.lastRecordCount
        let client = SessionLogClient(config: MemoryConfig())
        for _ in 0 ..< Self.recoveryAttempts {
            try? await Task.sleep(for: .seconds(1))
            guard !Task.isCancelled else { return }
            guard let detail = try? await client.detail(room: conversationID) else { continue }
            guard ChatSessionTranscriptStore.recordedReply(
                extending: interrupted.partialText,
                in: detail,
                since: baseline
            ) else { continue }
            transcript.loadHistory(from: detail)
            transport.discardInterruptedLiveCopy(in: transcript)
            return
        }
    }
}
