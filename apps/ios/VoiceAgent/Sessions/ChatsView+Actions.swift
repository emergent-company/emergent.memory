import SwiftUI

// MARK: - Row route

extension ChatsView {
    // MARK: - Subviews

    func expansionBinding(for parentID: String) -> Binding<Bool> {
        Binding(
            get: { expandedThreads.contains(parentID) },
            set: { isExpanded in
                if isExpanded {
                    expandedThreads.insert(parentID)
                } else {
                    expandedThreads.remove(parentID)
                }
            }
        )
    }

    var emptyView: some View {
        EmptyStateView(
            title: "chats.empty",
            description: "chats.empty.description",
            systemImage: "bubble.left.and.bubble.right"
        )
    }

    func failureView(_ message: String) -> some View {
        ErrorStateView(
            title: "chats.error",
            description: "chats.error.description",
            message: message,
            retryLabel: "sessions.retry",
            retry: {
                Task {
                    await store.load(agentID: agentFilter, includeArchived: includeArchived)
                }
            }
        )
    }

    /// Manual conversations open the chat room (they can continue); scheduled
    /// and board sessions open the read-only transcript.
    func route(for summary: SessionSummary) -> AppRoute {
        switch summary.origin {
        case "scheduled", "board":
            .session(SessionRoute(room: summary.room))
        default:
            .chat(ChatRoute(
                agentName: agentName,
                conversationID: summary.room,
                title: summary.title ?? summary.preview
            ))
        }
    }

    // MARK: - Selection / bulk archive

    func enterSelection() {
        selectedIDs.removeAll()
        isSelecting = true
    }

    func exitSelection() {
        isSelecting = false
        selectedIDs.removeAll()
    }

    /// Number of sub-chats the delete dialog must warn about for the pending
    /// parent. Zero for a leaf row (and for a parent whose children are not in
    /// the current list).
    var pendingDeleteChildCount: Int? {
        guard let id = pendingDelete?.room else { return nil }
        let count = store.sessions.filter { $0.parentConversationID == id }.count
        return count > 0 ? count : nil
    }

    func archiveSelected() {
        let ids = Array(selectedIDs)
        guard !ids.isEmpty else { return }
        let activeIDs = ids.filter {
            activeSession.liveConversationID == $0 || activeSession.launchConversationID == $0
        }
        // Leave selection mode immediately: the rows are already hidden, so the
        // list must not sit in a mode pointing at gone rows while requests run.
        exitSelection()
        Task {
            let result = await store.archive(ids: ids)
            for id in activeIDs {
                await endSessionIfActive(id)
            }
            if result.didFail {
                archiveError = String(
                    localized: "chats.archive.failed \(result.failedCount)",
                    comment: "Rows that could not be archived in a bulk archive."
                )
            }
        }
    }

    // MARK: - Lifecycle actions

    var deleteDialogBinding: Binding<Bool> {
        Binding(
            get: { pendingDelete != nil },
            set: {
                if !$0 {
                    pendingDelete = nil
                }
            }
        )
    }

    func archive(_ summary: SessionSummary) {
        Task {
            if await store.archive(id: summary.room) {
                await endSessionIfActive(summary.room)
            }
        }
    }

    func unarchive(_ summary: SessionSummary) {
        Task {
            if await store.unarchive(id: summary.room) {
                await endSessionIfActive(summary.room)
            }
        }
    }

    func delete(_ summary: SessionSummary) {
        pendingDelete = nil
        Task {
            if await store.delete(id: summary.room) {
                await endSessionIfActive(summary.room)
            }
        }
    }

    /// Ends the live room when the conversation it belongs to is archived or
    /// deleted, so the app never keeps talking in a room the user just closed.
    func endSessionIfActive(_ conversationID: String) async {
        guard activeSession.liveConversationID == conversationID
            || activeSession.launchConversationID == conversationID
        else { return }
        await controller.endSession()
    }
}
