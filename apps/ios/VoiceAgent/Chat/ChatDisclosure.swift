import SwiftUI

// MARK: - Chat disclosure

/// Which runs, thinking blocks and Sources footers the reader has opened or
/// closed, remembered outside the rows.
///
/// A step's `ToolCallInfo` is replaced wholesale when its result arrives, and a
/// `LazyVStack` rebuilds a row that scrolls out of view, so a row's own `@State`
/// is lost exactly when the content changes from running to settled. A group or
/// block that kept its state in the view would therefore fold shut under the
/// reader. The choices are remembered by each block's stable id — a group by its
/// first call's id, a thinking block by `ThinkingBlock.id` — so one row settling
/// can never change another row's choice.
@MainActor
@Observable
final class ChatDisclosure {
    private var states: [String: Bool] = [:]

    /// The reader's choice, or the caller's default: open while a step runs,
    /// closed once the run has settled.
    func isExpanded(_ groupID: String, default defaultValue: Bool) -> Bool {
        states[groupID] ?? defaultValue
    }

    func setExpanded(_ expanded: Bool, for groupID: String) {
        states[groupID] = expanded
    }

    /// Flips the choice exactly as tapping the summary line does, so a test can
    /// follow the same activation path the button uses (#2192).
    func toggle(_ groupID: String, default defaultValue: Bool) {
        setExpanded(!isExpanded(groupID, default: defaultValue), for: groupID)
    }

    /// Drops every choice, for a new conversation.
    func clear() {
        states.removeAll()
        thinkingStates.removeAll()
        sourcesStates.removeAll()
    }

    // MARK: Thinking blocks

    /// Opened reasoning rows by `ThinkingBlock.id`, kept here for the same
    /// reason: a block's view is rebuilt as deltas stream, so `@State` inside it
    /// would fold an opened trace shut mid-turn.
    private var thinkingStates: [String: Bool] = [:]

    func isThinkingExpanded(_ blockID: String) -> Bool {
        thinkingStates[blockID] ?? false
    }

    func setThinkingExpanded(_ expanded: Bool, for blockID: String) {
        thinkingStates[blockID] = expanded
    }

    func toggleThinking(_ blockID: String) {
        setThinkingExpanded(!isThinkingExpanded(blockID), for: blockID)
    }

    // MARK: Sources footers

    /// Opened Sources footers by message id, kept here like the runs so a
    /// recycled row cannot fold shut under the reader. Closed by default.
    private var sourcesStates: [String: Bool] = [:]

    func isSourcesExpanded(_ messageID: String) -> Bool {
        sourcesStates[messageID] ?? false
    }

    func setSourcesExpanded(_ expanded: Bool, for messageID: String) {
        sourcesStates[messageID] = expanded
    }
}

extension EnvironmentValues {
    /// Nil outside the chat list, where every row simply follows its default.
    @Entry var chatDisclosure: ChatDisclosure?
}
