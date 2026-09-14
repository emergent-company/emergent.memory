import SwiftUI

/// Dashboard page: the at-a-glance state of the active project — its identity,
/// aggregate object/schema/job stats, the local tools enabled for it, and its
/// agents — driven by `DashboardStore`. Connector/engine status now lives in
/// the window footer (`MainWindowView`).
struct DashboardPage: View {
    @EnvironmentObject private var settings: ConnectorSettings
    @EnvironmentObject private var accountStore: AccountStore
    @EnvironmentObject private var projectStore: ProjectStore
    @EnvironmentObject private var appState: AppState

    @StateObject private var store = DashboardStore()

    /// Last successfully loaded snapshot, kept so a refresh doesn't blank the
    /// page while the new data is in flight.
    @State private var lastSnapshot: DashboardSnapshot?
    @State private var lastProjectID: String?
    /// A token/session failure (the store itself owns server-side errors).
    @State private var tokenError: String?
    @State private var infoExpanded = false
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
            infoExpanded = false
            selectedAgent = nil
        }
        .sheet(item: $selectedAgent) { agent in
            AgentDetailView(agent: agent,
                            projectID: projectStore.activeProjectID ?? "",
                            serverURL: accountStore.activeEnvironment?.serverURLString
                                ?? settings.serverURL)
                .environmentObject(accountStore)
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
            if case .loaded(let snapshot) = store.state {
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
        if case .loading = store.state { return true }
        return false
    }

    // MARK: - Primary content (state machine)

    @ViewBuilder
    private var primaryContent: some View {
        if let tokenError {
            errorCard(tokenError)
        } else {
            switch store.state {
            case .idle:
                placeholderCard
            case .loading:
                if let snapshot = lastSnapshot {
                    snapshotPrimary(snapshot)
                } else {
                    loadingCard
                }
            case .loaded(let snapshot):
                snapshotPrimary(snapshot)
            case .error(let message):
                errorCard(message)
            }
        }
    }

    @ViewBuilder
    private func snapshotPrimary(_ snapshot: DashboardSnapshot) -> some View {
        projectCard(snapshot)
        statsGrid(snapshot)
        agentsCard(snapshot)
    }

    // MARK: - Project card

    private func projectCard(_ snapshot: DashboardSnapshot) -> some View {
        ConnectorCard {
            VStack(alignment: .leading, spacing: 14) {
                HStack(alignment: .top, spacing: 14) {
                    ZStack {
                        RoundedRectangle(cornerRadius: 10, style: .continuous)
                            .fill(Color.memoryPrimary.opacity(0.18))
                            .frame(width: 44, height: 44)
                        Image(systemName: "shippingbox")
                            .font(.system(size: 20, weight: .medium))
                            .foregroundStyle(Color.memoryPrimary)
                    }

                    VStack(alignment: .leading, spacing: 4) {
                        Text(projectName(snapshot))
                            .font(.title2.weight(.semibold))
                            .lineLimit(1)
                        // Organisation line is shown only when a name resolves;
                        // never a placeholder like "Unknown organisation".
                        if let organization = organizationDisplay(snapshot) {
                            HStack(alignment: .firstTextBaseline, spacing: 5) {
                                Image(systemName: "building.2")
                                    .font(.caption)
                                // Never tail-truncate the organisation: wrap to
                                // as many lines as needed.
                                Text(organization)
                                    .font(.subheadline)
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                            .foregroundStyle(.secondary)
                        }
                    }

                    Spacer(minLength: 0)

                    if isRefreshing {
                        ProgressView().controlSize(.small)
                    } else {
                        Button {
                            Task { await reload() }
                        } label: {
                            Label("Refresh", systemImage: "arrow.clockwise")
                        }
                        .buttonStyle(.borderless)
                    }
                }

                if let info = snapshot.projectInfo, !info.isEmpty {
                    Divider()
                    projectInfoView(info)
                }
            }
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
           let name = projectStore.organizationNames[orgID], !name.isEmpty {
            return name
        }
        if let project = projectStore.projects.first(where: { $0.id == projectStore.activeProjectID }) {
            return projectStore.organizationName(for: project)
        }
        return nil
    }

    private func projectInfoView(_ info: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("About")
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(info)
                .font(.callout)
                .lineLimit(infoExpanded ? nil : 3)
                .fixedSize(horizontal: false, vertical: true)
            if info.count > 180 {
                Button(infoExpanded ? "Show less" : "Show more") {
                    withAnimation(.easeInOut(duration: 0.15)) { infoExpanded.toggle() }
                }
                .buttonStyle(.link)
                .font(.caption)
            }
        }
    }

    // MARK: - Stats

    private struct StatTile: Identifiable {
        let id: String
        let title: String
        let systemImage: String
        let value: String
        let caption: String?
    }

    /// Fixed tile height so every stat box is identical regardless of content
    /// or window width.
    private let statTileHeight: CGFloat = 96
    /// Fixed column count (not `.adaptive`), so boxes never reflow into a
    /// different number of columns as the window resizes.
    private let statColumns = 3

    @ViewBuilder
    private func statsGrid(_ snapshot: DashboardSnapshot) -> some View {
        let tiles = statTiles(for: snapshot)
        if !tiles.isEmpty {
            LazyVGrid(
                columns: Array(repeating: GridItem(.flexible(), spacing: 12),
                               count: statColumns),
                alignment: .leading,
                spacing: 12
            ) {
                ForEach(tiles) { tile in
                    statTileView(tile)
                }
            }
        }
    }

    private func statTiles(for snapshot: DashboardSnapshot) -> [StatTile] {
        var tiles: [StatTile] = []

        if let objects = snapshot.resolvedObjectCount {
            tiles.append(StatTile(id: "objects",
                                  title: "Objects",
                                  systemImage: "cube",
                                  value: objects.formatted(),
                                  caption: nil))
        }
        if let relationships = snapshot.resolvedRelationshipCount {
            tiles.append(StatTile(id: "relationships",
                                  title: "Relationships",
                                  systemImage: "link",
                                  value: relationships.formatted(),
                                  caption: nil))
        }
        if let schema = snapshot.schemaStats {
            tiles.append(StatTile(id: "schema",
                                  title: "Schema Types",
                                  systemImage: "square.stack.3d.up",
                                  value: schema.totalTypes.formatted(),
                                  caption: "\(schema.totalObjects.formatted()) objects"))
        }
        if let stats = snapshot.projectStats {
            tiles.append(StatTile(id: "documents",
                                  title: "Documents",
                                  systemImage: "doc.text",
                                  value: stats.documentCount.formatted(),
                                  caption: nil))
            tiles.append(StatTile(id: "jobs",
                                  title: "Jobs",
                                  systemImage: "gearshape.2",
                                  value: stats.runningJobs.formatted(),
                                  caption: "\(stats.queuedJobs) queued · \(stats.totalJobs) total"))
        }

        return tiles
    }

    private func statTileView(_ tile: StatTile) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: tile.systemImage)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Text(tile.title.uppercased())
                    .font(.caption2.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
            }
            Spacer(minLength: 0)
            Text(tile.value)
                .font(.system(size: 26, weight: .semibold, design: .rounded))
                .monospacedDigit()
                .lineLimit(1)
                .minimumScaleFactor(0.7)
            // Always reserve the caption line so the value baseline stays
            // aligned across tiles that do and don't have a caption.
            Text(tile.caption ?? " ")
                .font(.caption)
                .foregroundStyle(.tertiary)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(14)
        .frame(maxWidth: .infinity, minHeight: statTileHeight, maxHeight: statTileHeight,
               alignment: .topLeading)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(.quaternary.opacity(0.45))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .strokeBorder(.quaternary.opacity(0.6))
        )
    }

    // MARK: - Configured tools

    private var enabledTools: [ToolCatalog.Tool] {
        let disabled = projectStore.activeDisabledTools
        return ToolCatalog.tools.filter { !disabled.contains($0.id) }
    }

    private var scopeText: String {
        guard projectStore.hasActiveProject else { return "Shared (no project selected)" }
        if let name = projectStore.activeProjectName { return "Applies to \(name)" }
        return "Applies to the active project"
    }

    private func iconName(for service: ToolCatalog.Service) -> String {
        switch service {
        case .notes: return "note.text"
        case .reminders: return "checklist"
        }
    }

    private var toolsCard: some View {
        ConnectorCard(title: "Configured Tools", systemImage: "puzzlepiece.extension") {
            VStack(alignment: .leading, spacing: 12) {
                if enabledTools.isEmpty {
                    Text("No local tools enabled. Turn some on in MCP Tools.")
                        .font(.callout)
                        .foregroundStyle(.secondary)
                } else {
                    // Single-column list: one equal-height row per tool.
                    VStack(alignment: .leading, spacing: 0) {
                        ForEach(enabledTools.indices, id: \.self) { index in
                            if index > enabledTools.startIndex { Divider() }
                            toolRow(enabledTools[index])
                        }
                    }
                }

                HStack(spacing: 8) {
                    Text(scopeText)
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                    Spacer(minLength: 0)
                    Button {
                        appState.selectedSidebarItem = .tools
                    } label: {
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

    // MARK: - Agents

    @ViewBuilder
    private func agentsCard(_ snapshot: DashboardSnapshot) -> some View {
        ConnectorCard(title: "Available Agents", systemImage: "person.2.wave.2") {
            if snapshot.agents.isEmpty {
                HStack(spacing: 12) {
                    Image(systemName: "wand.and.stars")
                        .foregroundStyle(.secondary)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("No agents yet")
                            .font(.callout)
                        Text("Agents created for this project will appear here.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 0)
                }
            } else {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(snapshot.agents.indices, id: \.self) { index in
                        if index > snapshot.agents.startIndex { Divider() }
                        Button {
                            selectedAgent = snapshot.agents[index]
                        } label: {
                            AgentSummaryRow(agent: snapshot.agents[index])
                        }
                        .buttonStyle(.plain)
                        .help("View agent details")
                    }
                }
            }
        }
    }

    // MARK: - Loading / error / placeholder

    private var loadingCard: some View {
        ConnectorCard {
            ConnectorLoadingRow(caption: "Loading dashboard…")
        }
    }

    private func errorCard(_ message: String) -> some View {
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
                Button("Retry") {
                    Task { await reload() }
                }
            }
        }
    }

    private var placeholderCard: some View {
        ConnectorCard {
            VStack(alignment: .leading, spacing: 10) {
                Image(systemName: accountStore.isEffectivelySignedIn ? "shippingbox" : "person.crop.circle.badge.questionmark")
                    .font(.largeTitle)
                    .foregroundStyle(.secondary)
                Text(accountStore.isEffectivelySignedIn ? "Select a project" : "Sign in to see your dashboard")
                    .font(.headline)
                Text(accountStore.isEffectivelySignedIn
                     ? "Choose a project in the toolbar to see its objects, agents, and tools."
                     : "Sign in with your Memory account to load project data.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                if accountStore.isEffectivelySignedIn {
                    Button("Open Project & Account") {
                        appState.selectedSidebarItem = .project
                    }
                }
            }
        }
    }
}

/// One clickable row in the Dashboard "Available Agents" card: the agent's
/// name, an enabled/disabled badge, a one-line description, and a tool hint.
/// Clicking the row opens ``AgentDetailView``. Hover paints a subtle highlight
/// so the whole row reads as a control.
private struct AgentSummaryRow: View {
    let agent: AgentDefinitionSummary

    @State private var isHovering = false

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            Image(systemName: "wand.and.stars")
                .foregroundStyle(.secondary)
                .frame(width: 20)

            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 8) {
                    Text(displayName)
                        .font(.callout.weight(.medium))
                        .lineLimit(1)
                    if let enabled = agent.enabled {
                        if enabled {
                            AgentStatusBadge(text: "Enabled", color: .green)
                        } else {
                            AgentStatusBadge(text: "Disabled", color: .secondary)
                        }
                    }
                    if agent.isDefault == true {
                        AgentStatusBadge(text: "Default", color: .memoryPrimary)
                    }
                }
                if let description = agent.description, !description.isEmpty {
                    Text(description)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }

            Spacer(minLength: 12)

            if let toolHint {
                Text(toolHint)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }

            Image(systemName: "chevron.right")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.tertiary)
        }
        .frame(minHeight: 40)
        .padding(.vertical, 8)
        .padding(.horizontal, 6)
        .contentShape(Rectangle())
        .background(
            RoundedRectangle(cornerRadius: 6, style: .continuous)
                .fill(.quaternary.opacity(isHovering ? 0.5 : 0))
        )
        .onHover { hovering in
            withAnimation(.easeOut(duration: 0.1)) { isHovering = hovering }
        }
    }

    private var displayName: String {
        agent.name.isEmpty ? "Untitled agent" : agent.name
    }

    /// "3 tools" / "No tools" from the list endpoint's `toolCount`. The summary
    /// carries no whitelist, so nil means "not reported" and we show nothing
    /// rather than a misleading label.
    private var toolHint: String? {
        guard let count = agent.toolCount else { return nil }
        return count == 0 ? "No tools" : "\(count) \(count == 1 ? "tool" : "tools")"
    }
}
