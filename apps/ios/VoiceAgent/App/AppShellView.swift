import SwiftUI

/// Route to an agent's second level (Conversation / Sessions).
struct AgentRoute: Hashable {
    let agentName: String
}

/// A navigation destination pushed onto the level-1 agents stack: either an
/// agent's second level or a session detail.
enum AppRoute: Hashable {
    case agent(AgentRoute)
    case session(SessionRoute)
}

/// Level 1: the two-tab root shell.
///
/// Tab "Agents" hosts a `NavigationStack` whose root is the control-plane
/// agent picker (a native list, with loading / error / read-only empty
/// states). Tapping an agent selects it (persisted under
/// `alfred.agentName`) and pushes its second level.
///
/// Tab "Settings" shows the connection config directly (no longer a sheet),
/// with the QR-auth scanner surfaced here because QR is app-level
/// authentication, not per-agent config.
///
/// The voice controller lives above this shell (in `AgentSessionRoot`), so
/// navigating here never tears down an active session — it is only rebuilt
/// when the selected agent changes.
struct AppShellView: View {
    @EnvironmentObject private var agentStore: AgentStore

    /// The navigation path is owned by `AgentSessionRoot` (which recreates
    /// this shell when the selected agent changes) so controller rebuilds
    /// cannot reset it.
    @Binding var path: [AppRoute]
    @State private var qrPresented = false

    init(path: Binding<[AppRoute]>) {
        self._path = path
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
        // presented from the shell: the Settings row and the picker's
        // load-failure state both open it.
        .sheet(isPresented: $qrPresented) { MemoryQRScannerView() }
        .task { await agentStore.load() }
    }

    // MARK: - Tabs

    /// Tab 1: the agent picker. Selecting an agent pushes its second level,
    /// which hides this root tab bar while it is on screen. The path holds
    /// `AppRoute` values (`.agent` for the second level, `.session` for a
    /// session detail), so the second level pushes session details onto this
    /// same stack.
    private var agentsTab: some View {
        NavigationStack(path: $path) {
            picker
                .navigationDestination(for: AppRoute.self) { route in
                    switch route {
                    case .agent(let r):
                        agentDestination(r)
                    case .session(let r):
                        sessionDestination(r)
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
        return SessionDetailView(room: route.room)
    }

    /// Tab 2: connection settings plus the QR-auth entry point.
    private var settingsTab: some View {
        AppSettingsTabView(qrPresented: $qrPresented)
            .tabItem { Label("settings.title", systemImage: "gearshape") }
    }

    // MARK: - Picker

    private var picker: some View {
        VStack(spacing: 0) {
            header()
                .padding(.horizontal)
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
        }
        .padding(.top, 2 * .grid)
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

    /// One native list row: agent name plus its flow type, using system
    /// semantic fonts and default insets.
    private func agentRow(_ agent: Agent) -> some View {
        VStack(alignment: .leading, spacing: 0.5 * .grid) {
            Text(agent.name)
                .font(.headline)
                .foregroundStyle(.fg0)
                .lineLimit(1)
            if let flowType = agent.flowType, !flowType.isEmpty {
                Text(LocalizedStringKey(flowType))
                    .font(.subheadline)
                    .foregroundStyle(.fg3)
                    .lineLimit(1)
            }
        }
    }

    /// Selects the agent (persists `alfred.agentName`) and pushes level 2.
    private func select(_ agent: Agent) {
        agentStore.select(agent)
        Log.nav.info("select agent \(agent.name)")
        path.append(.agent(AgentRoute(agentName: agent.name)))
    }

    // MARK: - States

    private func loadingView() -> some View {
        VStack(spacing: 3 * .grid) {
            Spacer()
            Spinner()
            Text("agents.loading")
                .font(.system(size: 13))
                .foregroundStyle(.fg3)
            Spacer()
        }
        .frame(maxWidth: .infinity, minHeight: 32 * .grid)
    }

    /// Load failure — often stale/invalid connection settings after a server
    /// change. Retry alone cannot fix a wrong host or key, so the setup-QR
    /// re-onboard path is offered first.
    private func failureView(_ message: String) -> some View {
        ContentUnavailableView {
            Label("agents.error", systemImage: "wifi.exclamationmark")
        } description: {
            VStack(spacing: 1 * .grid) {
                Text("agents.error.description")
                Text(message)
                    .font(.system(size: 12))
                    .foregroundStyle(.fg3)
            }
        } actions: {
            Button {
                qrPresented = true
            } label: {
                HStack(spacing: 1 * .grid) {
                    Image(systemName: "qrcode.viewfinder")
                    Text("agents.error.scanQR")
                }
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(.fgAccent)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .padding(.top, 2 * .grid)

            Button {
                Task { await agentStore.load() }
            } label: {
                Text("agents.retry")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(.fgAccent)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .padding(.top, 1 * .grid)
        }
    }

    /// Read-only empty state: no add-agent affordance on the phone, so this
    /// directs the user to configure agents from web/desktop.
    private func emptyView() -> some View {
        ContentUnavailableView {
            Label("agents.empty.readonly", systemImage: "person.crop.circle.badge.plus")
        } description: {
            Text("agents.empty.readonly.description")
        }
        .padding(.top, 4 * .grid)
    }
}

/// The level-1 Settings tab: the connection config screen with the QR-auth
/// entry point surfaced above it. QR is app-level authentication (backend
/// reachability), so it lives here rather than inside any agent's second
/// level. The scanner sheet itself is presented by `AppShellView`, which owns
/// `qrPresented` (the picker's load-failure state reuses the same sheet).
private struct AppSettingsTabView: View {
    @Binding var qrPresented: Bool

    var body: some View {
        VStack(spacing: 0) {
            qrRow()
            MemorySettingsView()
        }
        .background(.bg1)
    }

    private func qrRow() -> some View {
        Button {
            qrPresented = true
        } label: {
            HStack(spacing: 3 * .grid) {
                Image(systemName: "qrcode.viewfinder")
                    .font(.system(size: 17))
                    .foregroundStyle(.fgAccent)
                    .frame(width: 3 * .grid)
                VStack(alignment: .leading, spacing: 1 * .grid) {
                    Text("settings.qr.title")
                        .font(.system(size: 14, weight: .semibold))
                        .foregroundStyle(.fg0)
                    Text("settings.qr.footer")
                        .font(.system(size: 11))
                        .foregroundStyle(.fg3)
                }
                Spacer()
                Image(systemName: "chevron.right")
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(.fg3)
            }
            .padding(3 * .grid)
            .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusSmall))
        }
        .buttonStyle(.plain)
        .padding(.horizontal, 4 * .grid)
        .padding(.top, 2 * .grid)
        .padding(.bottom, 1 * .grid)
    }
}

#Preview {
    AppShellView(path: .constant([]))
        .environmentObject(AgentStore())
}
