import SwiftUI

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

    @Environment(ActiveSessionStore.self) var activeSession
    @EnvironmentObject var controller: MemorySessionController
    @Environment(\.dismiss) private var dismiss

    @StateObject var store = SessionsStore()
    /// Whether archived conversations are listed. Toggled from the header menu.
    @State var includeArchived = false
    /// Conversation awaiting delete confirmation.
    @State var pendingDelete: SessionSummary?
    /// Parents whose sub-chat disclosure is open. Empty by default; the branch
    /// belonging to the currently open chat is opened on load.
    @State var expandedThreads: Set<String> = []
    /// Multi-select bulk-archive mode.
    @State var isSelecting = false
    @State var selectedIDs: Set<String> = []
    /// Error shown after a partial bulk archive; the failed rows are already
    /// back in the list.
    @State var archiveError: String?

    var agentFilter: String? {
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
}
