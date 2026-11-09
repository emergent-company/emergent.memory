import Foundation

/// The transport's attached-run state, learned from conversation events
/// (`emergent.memory#437`, `emergent.memory#470`).
///
/// In its own file so the transport's turn machinery stays within the project's
/// file-length ratchet. These are the only writers of the attached-run flags;
/// the view reads them to gate the stop control and the header.
extension HTTPChatTransport {
    /// Applies the run facts from a `refresh` frame.
    ///
    /// A populated frame with no run id is the stale case: it clears the working
    /// state so a run that is gone cannot keep the bubble or stop control alive.
    /// The fieldless connect frame never reaches here (the decoder drops it), so
    /// it can never clear a turn this client is streaming.
    func applyRunRefresh(_ state: ConversationRunState) {
        guard !state.isBare else { return }
        liveRunID = state.runID
        isRunning = state.isActive
        // A stop is settled by the next refresh that is no longer "cancelling".
        if !state.isWorking || state.runStatus != "cancelling" {
            isCancelling = false
        }
        // The next refresh is the authoritative post-stop state.
        showsCancelledNotice = false
    }

    /// The subscription is gone (backgrounded or left the chat): nothing can be
    /// stopped or shown until a fresh refresh arrives. The id is kept while this
    /// client streams its own turn so a stop still has a handle.
    func clearAttachedRun() {
        resetAttachedRunState()
        if !isStreaming {
            liveRunID = nil
        }
    }

    /// Clears the attached-run flags for a new turn or a reset.
    func resetAttachedRunState() {
        isRunning = false
        isCancelling = false
        showsCancelledNotice = false
    }
}
