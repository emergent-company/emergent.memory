import Foundation

/// The live copy a text turn keeps in the view: keeping it in step with a
/// reload, and trading it for the recorded reply once the record supersedes it.
///
/// Split out of ``HTTPChatTransport`` so its turn machinery stays readable; they
/// go through the transport's live-copy seams rather than its private storage.
extension HTTPChatTransport {
    /// Reloads the live transcript into the view (used after the view resets).
    func refreshLiveTranscript(_ transcript: ChatSessionTranscriptStore) {
        transcript.syncLive(liveCopy)
    }

    /// Trades the truncated live copy for the finished reply the record now
    /// holds: the record renders on its own, so keeping the partial would stack
    /// a shorter duplicate underneath it. The banner goes with it — the drop is
    /// no longer the last word on the turn — but only if that banner is still
    /// this drop's: the slot is shared, and a later turn may have failed in the
    /// meantime (`emergent.memory#213`).
    func discardInterruptedLiveCopy(in transcript: ChatSessionTranscriptStore) {
        guard let interrupted = interruptedTurn else { return }
        removeLiveMessage(interrupted.liveMessageID)
        transcript.syncLive(liveCopy)
        clearInterruptedTurn()
        if errorMessage == interrupted.failureMessage {
            errorMessage = nil
        }
    }

    /// Clears the unanswered turn once its recorded reply is on screen, and the
    /// banner with it if that banner is still this turn's.
    ///
    /// Nothing of the turn ever streamed, so the record now supplies both
    /// halves: the live user copy would duplicate the recorded one, and the
    /// empty live turn has nothing left to draw beneath the reply
    /// (`emergent.memory#445`).
    func settleUnansweredTurn(
        in transcript: ChatSessionTranscriptStore,
        activity: ChatActivityStore
    ) {
        guard let unanswered = unansweredTurn else { return }
        clearUnansweredTurn()
        removeLiveMessage(unanswered.anchorMessageID)
        transcript.syncLive(liveCopy)
        activity.discardTurn(anchoredTo: unanswered.anchorMessageID)
        if errorMessage == unanswered.failureMessage {
            errorMessage = nil
        }
    }
}
