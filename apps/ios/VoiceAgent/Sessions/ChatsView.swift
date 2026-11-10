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
    /// Parents whose sub-chat disclosure is open. Empty by default; the branch
    /// belonging to the currently open chat is opened on load.
    @State private var expandedThreads: Set<String> = []
    /// Multi-select bulk-archive mode.
    @State private var isSelecting = false
    @State private var selectedIDs: Set<String> = []
    /// Error shown after a partial bulk archive; the failed rows are already
    /// back in the list.
    @State private var archiveError: String?

    private var agentFilter: String? {
        agentID.isEmpty ? nil : agentID
    }

    private var threads: [SessionThread] {
        SessionThreading.threads(from: store.sessions)
    }

    var body: some View {
        VStack(spacing: 0) {
            header
            content
        }
        .background(.bg1)
        .task(id: "\(agentID)|\(includeArchived)") {
            await store.load(agentID: agentFilter, includeArchived: includeArchived)
            revealOpenThread()
        }
        // Popping back from a chat does not re-fire the task above, so refresh
        // the list when the tab becomes visible again (a new conversation may
        // have just learned its id, or a lifecycle action may have landed).
        .onAppear {
            revealOpenThread()
            guard store.loadState != .idle else { return }
            Task {
                await store.load(agentID: agentFilter, includeArchived: includeArchived)
                revealOpenThread()
            }
        }
        .onChange(of: store.sessions) {
            revealOpenThread()
        }
        .onChange(of: activeSession.liveConversationID) {
            revealOpenThread()
        }
        .overlay(alignment: .bottom) {
            if let archiveError {
                Text(archiveError)
                    .font(MemoryFont.meta)
                    .foregroundStyle(.fg0)
                    .padding(.horizontal, 3 * .grid)
                    .padding(.vertical, 2 * .grid)
                    .background(.bg3, in: Capsule())
                    .padding(.bottom, 3 * .grid)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                    .task(id: archiveError) {
                        try? await Task.sleep(for: .seconds(4))
                        self.archiveError = nil
                    }
            }
        }
        .animation(.easeInOut(duration: 0.2), value: archiveError)
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
            if let count = pendingDeleteChildCount, count > 0 {
                Text("chats.delete.message.subchats \(count)")
            } else {
                Text("chats.delete.message")
            }
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

    /// Header actions. In normal mode: filter, new chat and Select. In selection
    /// mode: Select all and Archive (N), so the bulk action lives where the
    /// selection was started.
    @ViewBuilder
    private var headerActions: some View {
        if isSelecting {
            HStack(spacing: 1 * .grid) {
                Button {
                    if selectedIDs.count == store.sessions.count {
                        selectedIDs.removeAll()
                    } else {
                        selectedIDs = Set(store.sessions.map(\.id))
                    }
                } label: {
                    Image(systemName: selectedIDs.count == store.sessions.count ? "checkmark.circle.fill" : "checkmark.circle")
                        .font(.body.weight(.medium))
                        .foregroundStyle(.fgAccent)
                        .frame(minWidth: 44, minHeight: 44)
                        .contentShape(Rectangle())
                }
                .accessibilityLabel("chats.selectAll")

                Button {
                    archiveSelected()
                } label: {
                    Text("chats.archive.count \(selectedIDs.count)")
                        .font(MemoryFont.meta.weight(.semibold))
                        .foregroundStyle(selectedIDs.isEmpty ? .fg3 : .fgAccent)
                        .frame(minWidth: 44, minHeight: 44)
                        .contentShape(Rectangle())
                }
                .disabled(selectedIDs.isEmpty)
                .accessibilityLabel("chats.archive.count \(selectedIDs.count)")

                Button("chats.done") {
                    exitSelection()
                }
                .font(MemoryFont.meta.weight(.semibold))
                .foregroundStyle(.fg2)
                .frame(minWidth: 44, minHeight: 44)
            }
        } else {
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

                Button("chats.select") {
                    enterSelection()
                }
                .font(MemoryFont.meta.weight(.semibold))
                .foregroundStyle(.fg2)
                .frame(minWidth: 44, minHeight: 44)

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
        List(selection: $selectedIDs) {
            if isSelecting {
                // Selection mode is flat: every conversation, sub-chat included,
                // is directly selectable (a DisclosureGroup label cannot be a
                // selection target).
                ForEach(store.sessions) { summary in
                    ChatRow(summary: summary)
                        .tag(summary.id)
                        .listRowBackground(Color.clear)
                }
            } else {
                ForEach(threads) { thread in
                    sessionRow(thread.summary)
                    if !thread.children.isEmpty {
                        DisclosureGroup(isExpanded: expansionBinding(for: thread.id)) {
                            ForEach(thread.children) { child in
                                sessionRow(child)
                                    .padding(.leading, 3 * .grid)
                            }
                        } label: {
                            Text("chats.subchats.count \(thread.childCount)")
                                .font(MemoryFont.meta)
                                .foregroundStyle(.fg3)
                        }
                    }
                }
            }
        }
        .environment(\.editMode, .constant(isSelecting ? EditMode.active : EditMode.inactive))
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .refreshable {
            await store.load(agentID: agentFilter, includeArchived: includeArchived)
            revealOpenThread()
        }
    }

    private func sessionRow(_ summary: SessionSummary) -> some View {
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

    private func expansionBinding(for parentID: String) -> Binding<Bool> {
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

    /// Opens the branch containing the chat the user currently has open, so a
    /// sub-chat launched from elsewhere is visible (not stranded under a
    /// collapsed parent).
    private func revealOpenThread() {
        let openIDs = [activeSession.liveConversationID, activeSession.launchConversationID]
            .compactMap(\.self)
        guard !openIDs.isEmpty else { return }
        for summary in store.sessions where openIDs.contains(summary.room) {
            if let parent = summary.parentConversationID, !parent.isEmpty {
                expandedThreads.insert(parent)
            }
        }
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

    // MARK: - Selection / bulk archive

    private func enterSelection() {
        selectedIDs.removeAll()
        isSelecting = true
    }

    private func exitSelection() {
        isSelecting = false
        selectedIDs.removeAll()
    }

    /// Number of sub-chats the delete dialog must warn about for the pending
    /// parent. Zero for a leaf row (and for a parent whose children are not in
    /// the current list).
    private var pendingDeleteChildCount: Int? {
        guard let id = pendingDelete?.room else { return nil }
        let count = store.sessions.filter { $0.parentConversationID == id }.count
        return count > 0 ? count : nil
    }

    private func archiveSelected() {
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
                SessionRunBadge(summary: summary)
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

/// The row's run-state badge. It mirrors the web rail's three states from the
/// gateway `bucket` field: Working, Waiting on you (with the combined pending
/// decision count), and Failed. A done/absent bucket renders nothing.
private struct SessionRunBadge: View {
    let summary: SessionSummary

    private var pendingCount: Int {
        summary.pendingApprovals + summary.pendingQuestions
    }

    var body: some View {
        if let label {
            Text(label)
                .font(.caption2.weight(.semibold))
                .foregroundStyle(tint)
                .padding(.horizontal, 2 * .grid)
                .padding(.vertical, 1)
                .background(tint.opacity(0.12), in: Capsule())
        }
    }

    /// `nil` for done or an unknown/absent bucket, so no empty badge is drawn.
    private var label: String? {
        switch summary.bucket {
        case "needs_input":
            pendingCount > 0
                ? String(localized: "chats.badge.waiting.count \(pendingCount)")
                : String(localized: "chats.badge.waiting")
        case "running":
            String(localized: "chats.badge.working")
        case "failed":
            String(localized: "chats.badge.failed")
        default:
            nil
        }
    }

    private var tint: Color {
        switch summary.bucket {
        case "failed": .fgSerious
        case "needs_input": .fgModerate
        case "running": .fgAccent
        default: .fg3
        }
    }
}
