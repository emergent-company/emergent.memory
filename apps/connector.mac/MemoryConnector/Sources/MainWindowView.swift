import SwiftUI

/// Main application window: a `NavigationSplitView` whose sidebar separates
/// INFORMATION (dashboard, project & account) from SETTINGS (tools, permissions,
/// connection), with About pinned to the bottom. Replaces the old monolithic
/// settings form.
///
/// A slim footer bar shows a single project-connection status (dot + label),
/// driven by the account sign-in state and the connected project. Engine/hub
/// health (derived from `AppStatus`/`StatusMonitor`) is reachable only via the
/// footer's help tooltip; the menu-bar icon keeps its own behaviour.
struct MainWindowView: View {
    @SwiftUI.Environment(ConnectorSettings.self) private var settings
    @SwiftUI.Environment(AppState.self) private var appState
    @SwiftUI.Environment(AccountStore.self) private var accountStore
    @SwiftUI.Environment(IdentityStore.self) private var identity
    @SwiftUI.Environment(ProjectStore.self) private var projectStore
    @SwiftUI.Environment(EngineManager.self) private var engine
    @SwiftUI.Environment(StatusMonitor.self) private var statusMonitor

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
                // (avatar) control, which is the far-right item. Trailing
                // ToolbarItems render in declaration order.
                //
                // Neither control wants the macOS 26 shared glass capsule. They
                // are not buttons competing with the window's own actions: one
                // is window context (project) and the other is identity (the
                // avatar). A capsule around each reads as two toolbars; the
                // name, the chevron, the avatar and the hover highlight are the
                // affordances, and the panel header carries the identity. The
                // pre-26 toolbar never drew one, so the fallback branch keeps
                // the same shape.
                //
                // The project control exists only while effectively signed in:
                // with no account there is nothing to switch, and a project
                // trigger would render its own "Sign in" label beside the
                // account control's button — the same action, twice.
                if #available(macOS 26, *) {
                    if accountStore.isEffectivelySignedIn {
                        ToolbarItem(placement: .primaryAction) {
                            ProjectSwitcherView()
                        }
                        .sharedBackgroundVisibility(.hidden)
                        ToolbarSpacer(.fixed)
                    }
                    ToolbarItem(placement: .primaryAction) {
                        AccountToolbarView()
                    }
                    .sharedBackgroundVisibility(.hidden)
                } else {
                    if accountStore.isEffectivelySignedIn {
                        ToolbarItem(placement: .primaryAction) {
                            ProjectSwitcherView()
                        }
                    }
                    ToolbarItem(placement: .primaryAction) {
                        AccountToolbarView()
                    }
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

    /// Overall connector status (engine process + hub reachability). Surfaced
    /// only through the footer's help tooltip; never as visible text.
    private var status: AppStatus {
        AppStatus.derive(engine: engine.state, snapshot: statusMonitor.snapshot)
    }

    /// Project-connection summary driving the footer dot + label. A leftover
    /// connected-project id while signed out reads as "no projects connected".
    private var connectionSummary: ProjectConnectionSummary {
        ProjectConnectionSummary(
            signedIn: accountStore.isEffectivelySignedIn,
            connectedCount: projectStore.connectedProjectCount
        )
    }

    /// Maps the summary's dot state to a SwiftUI colour (the summary type is
    /// kept free of SwiftUI for testing).
    private var footerDotColor: Color {
        switch connectionSummary.dot {
        case .green: .green
        case .orange: .orange
        }
    }

    /// Optional trailing help detail: the hub's own status line, when known.
    private var hubHelpSuffix: String {
        guard let hub = statusMonitor.snapshot?.hubLine, !hub.isEmpty else { return "" }
        return " · " + hub
    }

    private var statusFooter: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(footerDotColor)
                .frame(width: 8, height: 8)
            Text(connectionSummary.label)
                .font(.caption)
                .foregroundStyle(connectionSummary.isConnected ? .primary : .secondary)
                .lineLimit(1)

            Spacer(minLength: 0)
        }
        .padding(.horizontal, 24)
        .frame(height: 24)
        .help(Text(status.label) + Text(verbatim: hubHelpSuffix))
        .accessibilityIdentifier("status.footer")
    }

    // MARK: - Sidebar

    private var sidebar: some View {
        List(selection: sidebarSelection) {
            ForEach(SidebarItem.Group.allCases) { group in
                Section(group.rawValue) {
                    ForEach(group.items) { item in
                        Label(item.title, systemImage: item.systemIcon)
                            .tag(item)
                            .accessibilityIdentifier("sidebar.\(item.rawValue)")
                    }
                }
            }
        }
        .listStyle(.sidebar)
        .navigationTitle("Memory")
        .safeAreaInset(edge: .bottom, spacing: 0) {
            aboutSidebarRow
        }
    }

    /// About is not project context, so it is pinned to the bottom of the
    /// sidebar and visually separated from the scrolled Information/Settings
    /// sections instead of living in the Settings cluster. A one-row `List`
    /// bound to the same selection keeps it a fully selectable sidebar row that
    /// highlights when About is selected.
    private var aboutSidebarRow: some View {
        VStack(spacing: 0) {
            Divider()
            List(selection: sidebarSelection) {
                Label(SidebarItem.about.title, systemImage: SidebarItem.about.systemIcon)
                    .tag(SidebarItem.about)
                    .accessibilityIdentifier("sidebar.about")
            }
            .listStyle(.sidebar)
            .scrollDisabled(true)
            .frame(height: 44)
        }
    }

    /// Single-selection `List` takes an optional binding; the app keeps a
    /// non-optional selection (a page is always shown), so adapt it here.
    private var sidebarSelection: Binding<SidebarItem?> {
        Binding(
            get: { appState.selectedSidebarItem },
            set: {
                if let newValue = $0 {
                    appState.selectedSidebarItem = newValue
                }
            }
        )
    }

    // MARK: - Detail

    /// AnyView-returning switch (Diane idiom) keeps the detail branch type
    /// shallow so `NavigationSplitView` + `List` initialization stays cheap.
    private var contentView: AnyView {
        switch appState.selectedSidebarItem {
        case .dashboard: AnyView(DashboardPage().accessibilityIdentifier("page.dashboard"))
        case .project: AnyView(ProjectAccountPage().accessibilityIdentifier("page.project"))
        case .tools: AnyView(ToolsPage().accessibilityIdentifier("page.tools"))
        case .mcpServers: AnyView(MCPServersPage().accessibilityIdentifier("page.mcpServers"))
        case .permissions: AnyView(PermissionsPage().accessibilityIdentifier("page.permissions"))
        case .connection: AnyView(ConnectionPage().accessibilityIdentifier("page.connection"))
        case .about: AnyView(AboutPage().accessibilityIdentifier("page.about"))
        }
    }
}
