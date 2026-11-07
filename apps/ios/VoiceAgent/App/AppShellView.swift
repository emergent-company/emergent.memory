import SwiftUI

/// Route to an agent's second level (Conversation / Chats).
struct AgentRoute: Hashable {
    let agentName: String
}

/// Route to one conversation opened as a full chat room (pushed from the
/// Chats list or the new-chat action). `conversationID` is nil for a new chat.
struct ChatRoute: Hashable {
    let agentName: String
    let conversationID: String?
    let title: String?
}

/// A navigation destination pushed onto the level-1 agents stack: an agent's
/// second level, a session detail (read-only scheduled/board transcript), a
/// full chat room, or an agent's object browser.
enum AppRoute: Hashable {
    case agent(AgentRoute)
    case session(SessionRoute)
    case chat(ChatRoute)
    case objects(ObjectRoute)
}

/// Level 1: the two-tab root shell.
///
/// Tab "Agents" hosts a `NavigationStack` whose root is the control-plane
/// agent picker (a native list, with loading / error / read-only empty
/// states). Tapping an agent selects it (persisted under
/// `alfred.agentName.<projectID>`) and pushes its second level.
///
/// Tab "Settings" lists the connected projects. Connecting a project is
/// app-level authentication and happens through the setup QR code, presented
/// from here and from the picker's no-project prompt.
///
/// The voice controller lives above this shell (in `AgentSessionRoot`), so
/// navigating here never tears down an active session — it is only rebuilt
/// when the selected agent changes.
struct AppShellView: View {
    @Environment(AgentStore.self) private var agentStore
    @Environment(ActiveSessionStore.self) private var activeSession
    @EnvironmentObject private var projectStore: MemoryProjectStore
    /// Captures the share extension queued, uploaded by the app (see
    /// ``ShareInbox``). The badge is the only entry point, and only when
    /// something is actually waiting.
    @EnvironmentObject private var shareInbox: ShareInbox

    /// The navigation path is owned by `AgentSessionRoot` (which recreates
    /// this shell when the selected agent changes) so controller rebuilds
    /// cannot reset it.
    @Binding var path: [AppRoute]
    @State private var qrPresented = false
    @State private var shareInboxPresented = false

    init(path: Binding<[AppRoute]>) {
        _path = path
    }

    /// Whether any project is connected. With none, the app is not configured
    /// at all: the picker shows the connect prompt instead of loading (or
    /// failing to load) agents for a project that does not exist yet.
    private var hasProject: Bool {
        !projectStore.connections.isEmpty
    }

    var body: some View {
        TabView {
            agentsTab
            settingsTab
        }
        .tint(.fgAccent)
        .toolbarBackground(.bg1, for: .tabBar)
        .toolbar(.hidden, for: .navigationBar)
        .background(.bg1)
        // QR onboarding is app-level authentication, so the scanner is
        // presented from the shell: the Settings project list and the picker's
        // no-project prompt both open it.
        .sheet(isPresented: $qrPresented) { MemoryQRScannerView() }
        .sheet(isPresented: $shareInboxPresented) { ShareInboxView() }
        .task {
            guard hasProject else { return }
            await agentStore.load()
        }
        .onChange(of: hasProject) { _, connected in
            if connected {
                Task { @MainActor in await agentStore.load() }
            } else {
                // Last project removed: no connection means no agents, and no
                // pushed screen may outlive the project it belonged to.
                agentStore.reset()
                path.removeAll()
            }
        }
        .onChange(of: projectStore.activeProjectID) { _, _ in
            // Switch from Settings as well as the picker header: a pushed
            // agent belongs to the previous project's connection.
            path.removeAll()
        }
    }

    // MARK: - Tabs

    /// Tab 1: the agent picker. Selecting an agent pushes its second level,
    /// which hides this root tab bar while it is on screen. The path holds
    /// `AppRoute` values (`.agent` for the second level, `.session` for a
    /// read-only transcript, `.chat` for a full chat room), so the second
    /// level pushes those onto this same stack.
    private var agentsTab: some View {
        NavigationStack(path: $path) {
            picker
                .navigationDestination(for: AppRoute.self) { route in
                    switch route {
                    case let .agent(agentRoute):
                        agentDestination(agentRoute)
                    case let .session(sessionRoute):
                        sessionDestination(sessionRoute)
                    case let .chat(chatRoute):
                        chatDestination(chatRoute)
                    case let .objects(objectRoute):
                        objectsDestination(objectRoute)
                    }
                }
        }
        .tabItem { Label("agents.title", systemImage: "person.2") }
    }

    /// Logs the push, then builds the agent's second level.
    private func agentDestination(_ route: AgentRoute) -> some View {
        Log.nav.info("push AgentLevelView \(route.agentName)")
        return AgentLevelView(agentName: route.agentName)
    }

    /// Logs the push, then builds the session detail.
    private func sessionDestination(_ route: SessionRoute) -> some View {
        Log.nav.info("push SessionDetailView \(route.room)")
        // A route value is the screen's identity: reusing the destination slot
        // for another session must not inherit the previous one's scroll state.
        return SessionDetailView(room: route.room)
            .id(route)
    }

    /// Logs the push, then builds the full chat room for the conversation.
    private func chatDestination(_ route: ChatRoute) -> some View {
        Log.nav.info("push ChatSessionView \(route.conversationID ?? "new")")
        // Same as the session detail: a different conversation is a different
        // screen, even when SwiftUI reuses the destination position in the
        // stack. Without this the transcript opens at the previous chat's
        // scroll offset.
        return ChatSessionView(route: route)
            .id(route)
    }

    /// Logs the push, then builds the agent's object browser.
    private func objectsDestination(_ route: ObjectRoute) -> some View {
        Log.nav.info("push ObjectBrowserView \(route.agentName)")
        return ObjectBrowserView(agentID: route.agentID, agentName: route.agentName)
    }

    /// Tab 2: the connected projects.
    private var settingsTab: some View {
        MemorySettingsView(onAddProject: { qrPresented = true })
            .tabItem { Label("settings.title", systemImage: "gearshape") }
    }

    // MARK: - Picker

    private var picker: some View {
        VStack(spacing: 0) {
            header()
                .padding(.horizontal)
            if hasProject {
                switch agentStore.loadState {
                case .idle, .loading:
                    loadingView()
                case let .failed(message):
                    failureView(message)
                case .loaded:
                    if agentStore.agents.isEmpty {
                        emptyView()
                    } else {
                        agentList()
                    }
                }
            } else {
                noProjectView()
            }
        }
        .background(.bg1)
    }

    private func header() -> some View {
        HStack(alignment: .center) {
            VStack(alignment: .leading, spacing: 1 * .grid) {
                Text("agents.title")
                    .font(.largeTitle.bold())
                    .foregroundStyle(.fg0)
                Text("agents.intro")
                    .font(.subheadline)
                    .foregroundStyle(.fg3)
            }
            Spacer()
            // No project, no switcher: a pill that says "No project" is a dead
            // control, and the picker body already offers the connect action.
            if hasProject {
                shareInboxButton
                MemoryProjectSwitcher(onSwitch: switchProject, onAddProject: { qrPresented = true })
                    .padding(.top, 2 * .grid)
            }
        }
        .padding(.top, 2 * .grid)
    }

    /// Shared captures waiting to upload. Only shown when there are some: an
    /// always-visible empty inbox is a badge nobody can clear.
    @ViewBuilder
    private var shareInboxButton: some View {
        if shareInbox.pendingCount > 0 {
            Button {
                shareInboxPresented = true
            } label: {
                Image(systemName: shareInbox.hasFailures ? "tray.full.fill" : "tray.full")
                    .font(.title3)
                    .foregroundStyle(shareInbox.hasFailures ? .orange : .fg2)
                    .padding(1.5 * .grid)
                    .background(.bg2, in: Circle())
                    .overlay(alignment: .topTrailing) {
                        Text("\(shareInbox.pendingCount)")
                            .font(.system(.caption2, weight: .bold))
                            .foregroundStyle(.bg1)
                            .padding(.horizontal, 1 * .grid)
                            .padding(.vertical, 0.25 * .grid)
                            .background(.fgAccent, in: Capsule())
                            .offset(x: 2, y: -2)
                    }
            }
            .buttonStyle(.plain)
            .accessibilityLabel(Text("share.inbox.accessibility"))
            .accessibilityValue(Text("\(shareInbox.pendingCount)"))
            .padding(.top, 2 * .grid)
        }
    }

    /// Switches the active project: mirrors the new connection and re-scopes
    /// the agent selection. The store posts `.memoryConfigChanged`, which
    /// rebuilds the session controller from the new credential; the shell's
    /// `activeProjectID` observer drops any pushed route of the old project.
    private func switchProject(_ connection: MemoryProjectConnection) {
        guard connection.projectID != projectStore.activeProjectID else { return }
        projectStore.setActive(projectID: connection.projectID)
        agentStore.setActiveProject(projectStore.activeProjectID)
        Task { @MainActor in
            await agentStore.load()
        }
    }

    /// The fetched agent list as native rows, newest server order preserved.
    private func agentList() -> some View {
        List {
            ForEach(agentStore.agents) { agent in
                Button {
                    select(agent)
                } label: {
                    agentRow(agent)
                }
            }
        }
        .listStyle(.insetGrouped)
    }

    /// One native list row: agent name, using system semantic fonts and
    /// default insets.
    private func agentRow(_ agent: Agent) -> some View {
        VStack(alignment: .leading, spacing: 0.5 * .grid) {
            Text(agent.name)
                .font(.headline)
                .foregroundStyle(.fg0)
                .lineLimit(1)
        }
    }

    /// Selects the agent (persists `alfred.agentName.<projectID>`), opens a
    /// fresh conversation for it, and pushes level 2. Resetting the open
    /// conversation here means the agent's Chats tab never inherits the
    /// previous agent's room.
    ///
    /// Re-selecting the agent whose *new* conversation is already open is a
    /// no-op: the room is already the requested one, and resetting it would
    /// drop the learned live id without rebuilding the controller.
    private func select(_ agent: Agent) {
        agentStore.select(agent)
        if activeSession.agentName != agent.name || activeSession.launchConversationID != nil {
            activeSession.open(agentName: agent.name, conversationID: nil)
        }
        Log.nav.info("select agent \(agent.name)")
        path.append(.agent(AgentRoute(agentName: agent.name)))
    }

    // MARK: - States

    private func loadingView() -> some View {
        LoadingStateView(title: "agents.loading", fillsHeight: false)
    }

    /// No project connected yet (first launch, or every project removed).
    ///
    /// This is the app's onboarding screen, so it is a call to action — never a
    /// load error. Nothing is known about agents before a project exists, and
    /// nothing is fetched.
    private func noProjectView() -> some View {
        EmptyStateView(
            title: "projects.onboarding.title",
            description: "projects.onboarding.description",
            systemImage: "qrcode.viewfinder",
            topPadding: 6 * .grid,
            actionLabel: "projects.onboarding.action",
            actionImage: "qrcode.viewfinder",
            action: { qrPresented = true }
        )
    }

    /// Load failure — a connected project whose control plane the app cannot
    /// reach (stale connection, rotated credential, server change). Retry
    /// alone cannot fix a wrong host, so re-onboarding is offered first.
    private func failureView(_ message: String) -> some View {
        ErrorStateView(
            title: "agents.error",
            description: "agents.error.description",
            message: message,
            retryLabel: "agents.retry",
            retry: { Task { await agentStore.load() } },
            leadingActionLabel: "agents.error.scanQR",
            leadingActionImage: "qrcode.viewfinder",
            leadingAction: { qrPresented = true }
        )
    }

    /// Read-only empty state: no add-agent affordance on the phone, so this
    /// directs the user to configure agents from web/desktop.
    private func emptyView() -> some View {
        EmptyStateView(
            title: "agents.empty.readonly",
            description: "agents.empty.readonly.description",
            systemImage: "person.crop.circle.badge.plus",
            topPadding: 4 * .grid
        )
    }
}

#Preview {
    AppShellView(path: .constant([]))
        .environment(AgentStore())
        .environment(ActiveSessionStore())
        .environmentObject(MemoryProjectStore())
        .environmentObject(ShareInbox())
}
