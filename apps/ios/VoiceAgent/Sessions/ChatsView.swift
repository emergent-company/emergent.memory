import SwiftUI

/// Route to one recorded session's read-only detail (pushed from Chats for
/// scheduled and board sessions).
struct SessionRoute: Hashable {
    let room: String
}

/// Chats destination (second level): the conversations for the selected agent,
/// most recently active first, via ``SessionsStore``.
///
/// A row opens a full chat room for a manual conversation (the user can keep
/// talking) and the read-only transcript for scheduled/board sessions. The
/// list also creates new chats and archives, unarchives or deletes an existing
/// conversation. Deleting asks first; archiving is immediate.
struct ChatsView: View {
    /// The second-level agent this list belongs to.
    let agentName: String
    /// The agent definition id used to scope the list, when known.
    let agentID: String

    @Environment(ActiveSessionStore.self) private var activeSession
    @EnvironmentObject private var controller: MemorySessionController
    @Environment(\.dismiss) private var dismiss

    @StateObject private var store = SessionsStore()
    /// Whether archived conversations are listed. Toggled from the header menu.
    @State private var includeArchived = false
    /// Conversation awaiting delete confirmation.
    @State private var pendingDelete: SessionSummary?

    private var agentFilter: String? {
        agentID.isEmpty ? nil : agentID
    }

    var body: some View {
        VStack(spacing: 0) {
            header
            content
        }
        .background(.bg1)
        .task(id: "\(agentID)|\(includeArchived)") {
            await store.load(agentID: agentFilter, includeArchived: includeArchived)
        }
        // Popping back from a chat does not re-fire the task above, so refresh
        // the list when the tab becomes visible again (a new conversation may
        // have just learned its id, or a lifecycle action may have landed).
        .onAppear {
            guard store.loadState != .idle else { return }
            Task {
                await store.load(agentID: agentFilter, includeArchived: includeArchived)
            }
        }
        .confirmationDialog(
            "chats.delete.title",
            isPresented: deleteDialogBinding,
            titleVisibility: .visible
        ) {
            if let summary = pendingDelete {
                Button("chats.delete", role: .destructive) {
                    delete(summary)
                }
            }
            Button("settings.cancel", role: .cancel) { pendingDelete = nil }
        } message: {
            Text("chats.delete.message")
        }
    }

    // MARK: - Header

    private var header: some View {
        ScreenHeader(
            title: Text("chats.title"),
            subtitle: Text(agentName),
            trailing: AnyView(headerActions)
        ) {
            dismiss()
        }
    }

    /// New chat plus the include-archived toggle. Both are header actions, so
    /// the list itself stays a plain, scannable set of conversations.
    private var headerActions: some View {
        HStack(spacing: 1 * .grid) {
            Menu {
                Toggle("chats.includeArchived", isOn: $includeArchived)
            } label: {
                Image(systemName: includeArchived ? "line.3.horizontal.decrease.circle.fill" : "line.3.horizontal.decrease.circle")
                    .font(.body.weight(.medium))
                    .foregroundStyle(includeArchived ? .fgAccent : .fg2)
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(Rectangle())
            }
            .accessibilityLabel("chats.filter")

            NavigationLink(value: AppRoute.chat(ChatRoute(
                agentName: agentName,
                conversationID: nil,
                title: nil
            ))) {
                Image(systemName: "square.and.pencil")
                    .font(.body.weight(.medium))
                    .foregroundStyle(.fgAccent)
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("chats.new")
        }
    }

    // MARK: - Content

    @ViewBuilder
    private var content: some View {
        if case let .failed(message) = store.loadState {
            failureView(message)
        } else if store.sessions.isEmpty {
            if store.loadState == .loading || store.loadState == .idle {
                LoadingStateView(title: "chats.loading")
            } else {
                emptyView
            }
        } else {
            list
        }
    }

    private var list: some View {
        List(store.sessions) { summary in
            NavigationLink(value: route(for: summary)) {
                ChatRow(summary: summary)
            }
            .listRowBackground(Color.clear)
            .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                Button(role: .destructive) {
                    pendingDelete = summary
                } label: {
                    Label("chats.delete", systemImage: "trash")
                }

                if summary.isArchived {
                    Button {
                        unarchive(summary)
                    } label: {
                        Label("chats.unarchive", systemImage: "tray.and.arrow.up")
                    }
                    .tint(.fgAccent)
                } else {
                    Button {
                        archive(summary)
                    } label: {
                        Label("chats.archive", systemImage: "archivebox")
                    }
                    .tint(.fgAccent)
                }
            }
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .refreshable { await store.load(agentID: agentFilter, includeArchived: includeArchived) }
    }

    private var emptyView: some View {
        EmptyStateView(
            title: "chats.empty",
            description: "chats.empty.description",
            systemImage: "bubble.left.and.bubble.right"
        )
    }

    private func failureView(_ message: String) -> some View {
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

    // MARK: - Row route

    /// Manual conversations open the chat room (they can continue); scheduled
    /// and board sessions open the read-only transcript.
    private func route(for summary: SessionSummary) -> AppRoute {
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

    // MARK: - Lifecycle actions

    private var deleteDialogBinding: Binding<Bool> {
        Binding(
            get: { pendingDelete != nil },
            set: {
                if !$0 {
                    pendingDelete = nil
                }
            }
        )
    }

    private func archive(_ summary: SessionSummary) {
        Task {
            if await store.archive(id: summary.room) {
                await endSessionIfActive(summary.room)
            }
        }
    }

    private func unarchive(_ summary: SessionSummary) {
        Task {
            if await store.unarchive(id: summary.room) {
                await endSessionIfActive(summary.room)
            }
        }
    }

    private func delete(_ summary: SessionSummary) {
        pendingDelete = nil
        Task {
            if await store.delete(id: summary.room) {
                await endSessionIfActive(summary.room)
            }
        }
    }

    /// Ends the live room when the conversation it belongs to is archived or
    /// deleted, so the app never keeps talking in a room the user just closed.
    private func endSessionIfActive(_ conversationID: String) async {
        guard activeSession.liveConversationID == conversationID
            || activeSession.launchConversationID == conversationID
        else { return }
        await controller.endSession()
    }
}

// MARK: - Row

/// One conversation row: where it came from, its title (or first message),
/// started time, turn/tool-call counts, and the archived/live state.
private struct ChatRow: View {
    let summary: SessionSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 2 * .grid) {
            HStack(spacing: 2 * .grid) {
                originBadge
                title
                if summary.isArchived {
                    archivedBadge
                }
                Spacer(minLength: 0)
                if summary.endedAt == nil {
                    liveBadge
                }
            }

            if showsSeparatePreview, let preview = summary.preview, !preview.isEmpty {
                Text(preview)
                    .font(MemoryFont.bodySmall)
                    .foregroundStyle(.fg2)
                    .lineLimit(2)
                    .multilineTextAlignment(.leading)
            }

            HStack(spacing: 2 * .grid) {
                if let startedAt = summary.startedAt {
                    Text("sessions.started \(DateFormatting.formatSessionDate(startedAt) ?? startedAt)")
                }
                Text("sessions.turns.count \(summary.turns)")
                Text("sessions.toolCalls.count \(summary.toolCalls)")
            }
            .font(MemoryFont.meta)
            .foregroundStyle(.fg3)
        }
        .padding(.vertical, 1 * .grid)
    }

    /// The title line: the conversation title when there is one, otherwise the
    /// first message, otherwise a neutral placeholder.
    @ViewBuilder
    private var title: some View {
        if let title = summary.title, !title.isEmpty {
            Text(title)
                .font(MemoryFont.cardHeader)
                .foregroundStyle(.fg0)
                .lineLimit(1)
        } else if let preview = summary.preview, !preview.isEmpty {
            Text(preview)
                .font(MemoryFont.cardHeader)
                .foregroundStyle(.fg0)
                .lineLimit(1)
        } else {
            Text("chats.preview.placeholder")
                .font(MemoryFont.cardHeader)
                .foregroundStyle(.fg3)
                .lineLimit(1)
        }
    }

    /// The preview becomes a second line only when the title is not already the
    /// first message.
    private var showsSeparatePreview: Bool {
        guard let title = summary.title, !title.isEmpty else { return false }
        return title != summary.preview
    }

    private var originBadge: some View {
        Text(originKey)
            .font(.caption2.weight(.semibold))
            .foregroundStyle(.fg2)
            .padding(.horizontal, 2 * .grid)
            .padding(.vertical, 1)
            .overlay(Capsule().stroke(.separator1, lineWidth: 1))
    }

    private var originKey: LocalizedStringKey {
        switch summary.origin {
        case "scheduled":
            "chats.origin.scheduled"
        case "board":
            "chats.origin.board"
        default:
            "chats.origin.chat"
        }
    }

    private var archivedBadge: some View {
        Label("chats.archived", systemImage: "archivebox")
            .font(MemoryFont.meta)
            .foregroundStyle(.fg3)
            .labelStyle(.titleAndIcon)
    }

    private var liveBadge: some View {
        HStack(spacing: 1 * .grid) {
            Circle()
                .fill(.fgSuccess)
                .frame(width: 6, height: 6)
            Text("sessions.live")
        }
        .font(MemoryFont.meta)
        .foregroundStyle(.fg3)
    }
}
