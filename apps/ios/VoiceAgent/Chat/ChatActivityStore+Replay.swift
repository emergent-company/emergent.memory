import Foundation

/// Applying the conversation events stream's retained in-flight frames.
extension ChatActivityStore {
    /// Applies `live_replay` frames from the conversation events stream.
    ///
    /// Only open thinking, running tools and subagents are retained server-side,
    /// never text, so this never writes a duplicate reply. The attached turn (if
    /// any) must be open before this runs so the rows have a turn to land in.
    func applyReplay(_ events: [ChatStreamEvent]) {
        for event in events {
            switch event {
            case let .activity(chatEvent):
                apply(chatEvent)
            case let .replay(nested):
                applyReplay(nested)
            default:
                break
            }
        }
    }
}
