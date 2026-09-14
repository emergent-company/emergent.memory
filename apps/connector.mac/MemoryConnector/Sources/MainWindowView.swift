import SwiftUI

/// Main application window: a `NavigationSplitView` whose sidebar separates
/// INFORMATION (dashboard, project & account) from SETTINGS (tools,
/// permissions, connection, about). Replaces the old monolithic settings form.
///
/// A slim footer bar shows overall connector status (engine + hub), derived
/// from `AppStatus`/`StatusMonitor`; the menu-bar icon keeps its own behaviour.
struct MainWindowView: View {
    @EnvironmentObject private var settings: ConnectorSettings
    @EnvironmentObject private var appState: AppState
    @EnvironmentObject private var identity: IdentityStore
    @EnvironmentObject private var projectStore: ProjectStore

    @ObservedObject private var engine = EngineManager.shared
    @ObservedObject private var statusMonitor = StatusMonitor.shared

    var body: some View {
        VStack(spacing: 0) {
            NavigationSplitView {
                sidebar
                    .navigationSplitViewColumnWidth(min: 190, ideal: 210, max: 280)
            } detail: {
                contentView
                    .frame(minWidth: 560, maxWidth: .infinity, maxHeight: .infinity)
            }
            .frame(minWidth: 780, minHeight: 520)
            .toolbar {
                // Trailing group: project picker immediately LEFT of the account
                // (person) control, which is the far-right item. Trailing
                // ToolbarItems render in declaration order.
                ToolbarItem(placement: .primaryAction) {
                    ProjectSwitcherView()
                }
                ToolbarItem(placement: .primaryAction) {
                    AccountToolbarView()
                }
            }
            .onAppear {
                // First run: adopt an existing CLI-created engine config before
                // any page reads settings (never overwrites a saved app profile).
                settings.importExistingConfigIfNeeded(configURL: EngineConfigSync.configURL)
            }

            Divider()
            statusFooter
        }
    }

    // MARK: - Footer status

    /// Overall connector status (engine process + hub reachability).
    private var status: AppStatus {
        AppStatus.derive(engine: engine.state, snapshot: statusMonitor.snapshot)
    }

    /// Footer dot colours: green connected, orange connecting/starting, red
    /// error, gray disconnected/stopped. (Independent of the menu-bar icon,
    /// which keeps `AppStatus.color`.)
    private var footerColor: Color {
        switch status {
        case .connected:    return .green
        case .connecting:   return .orange
        case .error:        return .red
        case .disconnected: return .secondary
        }
    }

    /// Optional trailing detail: the hub's own status line, else the engine
    /// instance id. Kept to a single elided line.
    private var footerDetail: String? {
        guard let snapshot = statusMonitor.snapshot else { return nil }
        if !snapshot.hubLine.isEmpty { return snapshot.hubLine }
        if !snapshot.instanceID.isEmpty { return snapshot.instanceID }
        return nil
    }

    /// `Connected: <project>` / `Not connected`, for the engine relay.
    private var connectionLabel: String {
        guard projectStore.hasConnectedProject else { return "Not connected" }
        let name = projectStore.projects
            .first { $0.id == projectStore.connectedProjectID }?
            .name
        return "Connected: \(name ?? "a project")"
    }

    private var statusFooter: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(footerColor)
                .frame(width: 8, height: 8)
            Text(status.label)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)

            Text("·")
                .font(.caption)
                .foregroundStyle(.tertiary)

            Image(systemName: projectStore.hasConnectedProject ? "bolt.fill" : "bolt.slash")
                .font(.caption2)
                .foregroundStyle(projectStore.hasConnectedProject ? Color.green : Color.secondary)
            Text(connectionLabel)
                .font(.caption)
                .foregroundStyle(projectStore.hasConnectedProject ? .primary : .secondary)
                .lineLimit(1)

            Spacer(minLength: 0)
            if let detail = footerDetail {
                Text(detail)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .lineLimit(1)
            }
        }
        .padding(.horizontal, 24)
        .frame(height: 24)
        .help("\(status.label) · \(connectionLabel)")
    }

    // MARK: - Sidebar

    private var sidebar: some View {
        List(selection: sidebarSelection) {
            ForEach(SidebarItem.Group.allCases) { group in
                Section(group.rawValue) {
                    ForEach(group.items) { item in
                        Label(item.title, systemImage: item.systemIcon)
                            .tag(item)
                    }
                }
            }
        }
        .listStyle(.sidebar)
        .navigationTitle("Memory")
    }

    /// Single-selection `List` takes an optional binding; the app keeps a
    /// non-optional selection (a page is always shown), so adapt it here.
    private var sidebarSelection: Binding<SidebarItem?> {
        Binding(
            get: { appState.selectedSidebarItem },
            set: { if let newValue = $0 { appState.selectedSidebarItem = newValue } }
        )
    }

    // MARK: - Detail

    /// AnyView-returning switch (Diane idiom) keeps the detail branch type
    /// shallow so `NavigationSplitView` + `List` initialization stays cheap.
    private var contentView: AnyView {
        switch appState.selectedSidebarItem {
        case .dashboard:   return AnyView(DashboardPage())
        case .project:     return AnyView(ProjectAccountPage())
        case .tools:       return AnyView(ToolsPage())
        case .mcpServers:  return AnyView(MCPServersPage())
        case .permissions: return AnyView(PermissionsPage())
        case .connection:  return AnyView(ConnectionPage())
        case .about:       return AnyView(AboutPage())
        }
    }
}
