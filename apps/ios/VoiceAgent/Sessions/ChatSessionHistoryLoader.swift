import Foundation

/// Loads one conversation's recorded transcript into ``ChatSessionTranscriptStore``.
///
/// The fetch used to live in the view, keyed on the loaded conversation id. Two
/// things followed from that, and both are why this type exists:
///
/// - a cancelled request was recorded as a user-facing failure, so opening a
///   conversation could show "Could not reach the session log …: cancelled" for
///   a request the app itself had cancelled when it rebuilt the controller; and
/// - the id was marked loaded *before* the request, so the retry the banner
///   offers was skipped by the "already loaded" guard and the banner stayed.
///
/// The rules, stated rather than implied:
/// - ``loadedConversationID`` is set only when the fetch succeeded, so a failure
///   or a cancellation never marks the conversation as loaded;
/// - cancellation is not a failure (`Task` cancellation or `URLError.cancelled`):
///   the loading flag stops and no error is surfaced;
/// - ``generation`` makes a late response a no-op, so a fetch that finishes
///   after the transcript moved to another conversation cannot write into it;
/// - a real failure is reported once and leaves the conversation unloaded, so
///   the next trigger (reopening the chat, the retry button) fetches again.
@MainActor
final class ChatSessionHistoryLoader {
    private let client: any SessionLogFetching
    /// The conversation whose records are currently applied.
    private(set) var loadedConversationID: String?
    /// Invalidates in-flight loads on ``reset()`` and on every new load.
    private var generation = 0

    /// The client seam keeps this testable without a network; the default is the
    /// live session-log client.
    init(client: any SessionLogFetching = SessionLogClient(config: MemoryConfig())) {
        self.client = client
    }

    /// Forgets the loaded conversation, so the next load fetches again.
    func reset() {
        generation += 1
        loadedConversationID = nil
    }

    /// Fetches `GET /api/session?room=` and applies it to `transcript`.
    ///
    /// Returns whether records were applied. `force` refetches a conversation
    /// that is already loaded, because a call in between may have appended
    /// records.
    @discardableResult
    func load(
        conversationID: String,
        force: Bool = false,
        into transcript: ChatSessionTranscriptStore
    ) async -> Bool {
        if !force, loadedConversationID == conversationID {
            return false
        }
        generation += 1
        let loadGeneration = generation
        transcript.beginHistoryLoad()
        do {
            let detail = try await client.detail(room: conversationID)
            // A reset or a newer load superseded this one while it was in
            // flight; its records belong to a transcript that has moved on.
            guard loadGeneration == generation, !Cancellation.isCurrentTaskCancelled else {
                abandon(loadGeneration, transcript)
                return false
            }
            loadedConversationID = conversationID
            transcript.loadHistory(from: detail)
            return true
        } catch {
            guard loadGeneration == generation else { return false }
            if Cancellation.isCancellation(error) || Cancellation.isCurrentTaskCancelled {
                abandon(loadGeneration, transcript)
            } else {
                loadedConversationID = nil
                transcript.historyLoadFailed(error.localizedDescription)
            }
            return false
        }
    }

    /// Drops a load that is still the current one, without reporting it.
    private func abandon(_ loadGeneration: Int, _ transcript: ChatSessionTranscriptStore) {
        guard loadGeneration == generation else { return }
        loadedConversationID = nil
        transcript.cancelHistoryLoad()
    }
}
