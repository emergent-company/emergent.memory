import SwiftUI

/// Dashboard page: the at-a-glance state of the active project — its identity,
/// aggregate object/schema/job stats, the local tools enabled for it, and its
/// agents — driven by `DashboardStore`. Connector/engine status now lives in
/// the window footer (`MainWindowView`).
struct DashboardPage: View {
    @SwiftUI.Environment(ConnectorSettings.self) private var settings
    @SwiftUI.Environment(AccountStore.self) private var accountStore
    @SwiftUI.Environment(ProjectStore.self) private var projectStore
    @SwiftUI.Environment(AppState.self) private var appState

    @State private var store = DashboardStore()

    /// Last successfully loaded snapshot, kept so a refresh doesn't blank the
    /// page while the new data is in flight.
    @State private var lastSnapshot: DashboardSnapshot?
    @State private var lastProjectID: String?
    /// A token/session failure (the store itself owns server-side errors).
    @State private var tokenError: String?
    /// The agent whose detail sheet is open, if any (row click).
    @State private var selectedAgent: AgentDefinitionSummary?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                if accountStore.isEffectivelySignedIn {
                    ProjectConnectionControl()
                }
                primaryContent
                toolsCard
            }
            .padding(24)
            .frame(maxWidth: 780, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .center)
        }
        .navigationTitle("Dashboard")
        .task(id: loadKey) { await reload() }
        .onChange(of: projectStore.activeProjectID) { _, _ in
            selectedAgent = nil
        }
        .sheet(item: $selectedAgent) { agent in
            AgentDetailView(agent: agent,
                            projectID: projectStore.activeProjectID ?? "",
                            serverURL: accountStore.activeEnvironment?.serverURLString
                                ?? settings.serverURL)
                .environment(accountStore)
        }
    }

    // MARK: - Loading

    /// Re-runs the `.task` whenever the server, active project, or session
    /// changes. An `Equatable` key is the idiomatic macOS 15 reload trigger.
    private struct LoadKey: Equatable {
        let serverURL: String
        let projectID: String?
        let signedIn: Bool
    }

    private var loadKey: LoadKey {
        LoadKey(serverURL: accountStore.activeEnvironment?.serverURLString ?? settings.serverURL,
                projectID: projectStore.activeProjectID,
                signedIn: accountStore.isEffectivelySignedIn)
    }

    private func reload() async {
        tokenError = nil

        guard accountStore.isEffectivelySignedIn else {
            store.clear()
            lastSnapshot = nil
            lastProjectID = nil
            return
        }
        guard let projectID = projectStore.activeProjectID, !projectID.isEmpty else {
            store.clear()
            lastSnapshot = nil
            lastProjectID = nil
            return
        }
        let serverURL = (accountStore.activeEnvironment?.serverURLString ?? settings.serverURL)
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard !serverURL.isEmpty else {
            store.clear()
            lastSnapshot = nil
            lastProjectID = nil
            return
        }

        // Don't carry the previous project's numbers across a switch.
        if projectID != lastProjectID {
            lastSnapshot = nil
            lastProjectID = projectID
        }

        do {
            let token = try await accountStore.currentAccessToken()
            await store.load(projectID: projectID, serverURL: serverURL, accessToken: token)
            if case let .loaded(snapshot) = store.state {
                lastSnapshot = snapshot
                // Backfill the org id → name map so other surfaces (project
                // grouping) can show the organisation too.
                if let orgID = snapshot.orgID, let name = snapshot.organizationName {
                    projectStore.registerOrganizationName(name, forOrgID: orgID)
                }
            }
        } catch {
            tokenError = error.localizedDescription
        }
    }

    private var isRefreshing: Bool {
        if case .loading = store.state {
            return true
        }
        return false
    }

    // MARK: - Primary content (state machine)

    @ViewBuilder
    private var primaryContent: some View {
        if let tokenError {
            DashboardErrorCard(message: tokenError, onRetry: { Task { await reload() } })
        } else {
            switch store.state {
            case .idle:
                DashboardPlaceholderCard(
                    signedIn: accountStore.isEffectivelySignedIn,
                    onOpenProject: { appState.selectedSidebarItem = .project }
                )
            case .loading:
                if let snapshot = lastSnapshot {
                    snapshotPrimary(snapshot)
                } else {
                    DashboardLoadingCard()
                }
            case let .loaded(snapshot):
                snapshotPrimary(snapshot)
            case let .error(message):
                DashboardErrorCard(message: message, onRetry: { Task { await reload() } })
            }
        }
    }

    @ViewBuilder
    private func snapshotPrimary(_ snapshot: DashboardSnapshot) -> some View {
        DashboardProjectCard(
            snapshot: snapshot,
            projectName: projectName(snapshot),
            organizationName: organizationDisplay(snapshot),
            isRefreshing: isRefreshing,
            projectID: projectStore.activeProjectID,
            onRefresh: { Task { await reload() } }
        )
        DashboardStatsGrid(snapshot: snapshot)
        DashboardAgentsCard(agents: snapshot.agents) { agent in
            selectedAgent = agent
        }
    }

    private func projectName(_ snapshot: DashboardSnapshot) -> String {
        snapshot.projectName
            ?? projectStore.activeProjectName
            ?? "Active project"
    }

    /// Org name for the dashboard's project, preferring the value the store
    /// resolved from `GET /api/orgs`, then the loaded org map, falling back to
    /// the active `ProjectInfo`. Returns nil when no name is known, so the org
    /// line is omitted rather than shown as a placeholder.
    private func organizationDisplay(_ snapshot: DashboardSnapshot) -> String? {
        if let name = snapshot.organizationName, !name.isEmpty {
            return name
        }
        if let orgID = snapshot.orgID,
           let name = projectStore.organizationNames[orgID], !name.isEmpty
        {
            return name
        }
        if let project = projectStore.projects.first(where: { $0.id == projectStore.activeProjectID }) {
            return projectStore.organizationName(for: project)
        }
        return nil
    }

    // MARK: - Configured tools

    private var enabledTools: [ToolCatalog.Tool] {
        let disabled = projectStore.activeDisabledTools
        return ToolCatalog.tools.filter { !disabled.contains($0.id) }
    }

    private var scopeText: LocalizedStringResource {
        guard projectStore.hasActiveProject else { return "Shared (no project selected)" }
        if let name = projectStore.activeProjectName {
            return "Applies to \(name)"
        }
        return "Applies to the active project"
    }

    private var toolsCard: some View {
        DashboardToolsCard(
            enabledTools: enabledTools,
            scopeText: scopeText,
            onOpenTools: { appState.selectedSidebarItem = .tools }
        )
    }
}

// MARK: - Configured tools

/// The dashboard's "Configured Tools" card: the local tools enabled for the
/// active project, plus the scope caption and a shortcut to the tools page.
struct DashboardToolsCard: View {
    let enabledTools: [ToolCatalog.Tool]
    let scopeText: LocalizedStringResource
    let onOpenTools: () -> Void

    var body: some View {
        ConnectorCard(title: "Configured Tools", systemImage: "puzzlepiece.extension") {
            VStack(alignment: .leading, spacing: 12) {
                if enabledTools.isEmpty {
                    Text("No local tools enabled. Turn some on in MCP Tools.")
                        .font(.callout)
                        .foregroundStyle(.secondary)
                } else {
                    // Single-column list: one equal-height row per tool.
                    VStack(alignment: .leading, spacing: 0) {
                        ForEach(enabledTools) { tool in
                            if tool.id != enabledTools.first?.id {
                                Divider()
                            }
                            toolRow(tool)
                        }
                    }
                }

                HStack(spacing: 8) {
                    Text(scopeText)
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                    Spacer(minLength: 0)
                    Button(action: onOpenTools) {
                        Label("Open MCP Tools", systemImage: "arrow.up.forward.app")
                    }
                    .buttonStyle(.link)
                    .font(.caption)
                }
            }
        }
    }

    /// One tool row: icon, name + summary subtitle, and service. Fixed minimum
    /// height keeps every row the same size in the single-column list.
    private func toolRow(_ tool: ToolCatalog.Tool) -> some View {
        HStack(spacing: 10) {
            Image(systemName: iconName(for: tool.service))
                .foregroundStyle(.secondary)
                .frame(width: 22)
            VStack(alignment: .leading, spacing: 2) {
                Text(tool.displayName)
                    .font(.callout)
                Text(tool.summary)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer(minLength: 8)
            Text(tool.service.displayName)
                .font(.caption)
                .foregroundStyle(.tertiary)
                .lineLimit(1)
        }
        .frame(minHeight: 40)
        .padding(.vertical, 2)
    }

    private func iconName(for service: ToolCatalog.Service) -> String {
        switch service {
        case .notes: "note.text"
        case .reminders: "checklist"
        }
    }
}

// MARK: - Loading / error / placeholder

struct DashboardLoadingCard: View {
    var body: some View {
        ConnectorCard {
            ConnectorLoadingRow(caption: "Loading dashboard…")
        }
    }
}

struct DashboardErrorCard: View {
    let message: String
    let onRetry: () -> Void

    var body: some View {
        ConnectorCard {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(.title3)
                    .foregroundStyle(.orange)
                VStack(alignment: .leading, spacing: 6) {
                    Text("Couldn't load dashboard")
                        .font(.headline)
                    Text(message)
                        .font(.callout)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 0)
                Button("Retry", action: onRetry)
            }
        }
    }
}

struct DashboardPlaceholderCard: View {
    let signedIn: Bool
    let onOpenProject: () -> Void

    var body: some View {
        ConnectorCard {
            VStack(alignment: .leading, spacing: 10) {
                Image(systemName: signedIn ? "shippingbox" : "person.crop.circle.badge.questionmark")
                    .font(.largeTitle)
                    .foregroundStyle(.secondary)
                Text(signedIn ? "Select a project" : "Sign in to see your dashboard")
                    .font(.headline)
                Text(signedIn
                    ? "Choose a project in the toolbar to see its objects, agents, and tools."
                    : "Sign in with your Memory account to load project data.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                if signedIn {
                    Button("Open Project & Account", action: onOpenProject)
                }
            }
        }
    }
}
