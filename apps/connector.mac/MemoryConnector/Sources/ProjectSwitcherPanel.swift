import AppKit
import SwiftUI

/// The project context panel: who you are working as, every project grouped by
/// organisation, and the actions that belong to that context.
///
/// Presented in a `.popover` from the toolbar trigger and embedded inline on the
/// Project page card, so both surfaces show the same list with the same row
/// behaviour. The panel owns no state of its own beyond keyboard highlight: the
/// stores stay the source of truth.
struct ProjectSwitcherPanel: View {
    @SwiftUI.Environment(AccountStore.self) private var accountStore
    @SwiftUI.Environment(ProjectStore.self) private var projectStore
    @SwiftUI.Environment(RecentProjectsStore.self) private var recentProjects
    @SwiftUI.Environment(AppState.self) private var appState

    /// Closes the host popover before an action navigates away. `nil` when the
    /// panel is embedded (the Project page card), where there is nothing to
    /// dismiss.
    var onDismiss: (() -> Void)?
    /// The page card hides "Manage projects…": that page *is* the manager.
    var showsManageAction = true
    /// The page card already shows the active project above the list, so the
    /// panel's own header would repeat it.
    var showsHeader = true

    @State private var highlightedID: String?
    @FocusState private var isFocused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if showsHeader {
                header
            }
            if projectStore.projects.isEmpty {
                status
            } else {
                rows
                SwitcherDivider()
            }
            actions
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .focusable()
        .focused($isFocused)
        .focusEffectDisabled()
        .onMoveCommand(perform: move)
        .onKeyPress(.return) {
            guard let id = highlightedID else { return .ignored }
            switchProject(to: id)
            return .handled
        }
        .onAppear {
            // Only take focus in the popover; the page card embeds this list and
            // must not steal the keyboard from the page.
            guard onDismiss != nil else { return }
            isFocused = true
            highlightedID = projectStore.activeProjectID
        }
    }

    // MARK: - Header

    @ViewBuilder
    private var header: some View {
        if let project = projectStore.activeProject {
            SwitcherIdentityHeader(
                icon: .symbol("folder"),
                title: displayName(project),
                subtitle: projectStore.organizationName(for: project),
                note: projectStore.isConnected(project.id) ? String(localized: "Connected — serving its tools") : nil
            )
        } else {
            SwitcherIdentityHeader(
                icon: .symbol("folder"),
                title: String(localized: "No project selected"),
                subtitle: nil
            )
        }
    }

    // MARK: - Rows

    private var rows: some View {
        ForEach(sections) { section in
            if let title = section.title {
                SwitcherSectionHeader(verbatim: title)
            }
            ForEach(section.rows) { row in
                SwitcherRow(
                    icon: .none,
                    title: row.title,
                    subtitle: row.subtitle,
                    isActive: row.isActive,
                    isConnected: row.isConnected,
                    isHighlighted: highlightedID == row.id,
                    isEnabled: projectStore.state != .loading,
                    action: { switchProject(to: row.id) }
                )
            }
        }
    }

    private var sections: [ProjectSwitcherSection] {
        ProjectSwitcherModel.sections(
            groups: projectStore.projectsByOrganization,
            recents: recentProjects.resolved(in: projectStore.projects),
            organizationNames: projectStore.organizationNames,
            connectedIDs: Set(projectStore.connectedProjects.map(\.id)),
            activeID: projectStore.activeProjectID
        )
    }

    // MARK: - Status

    @ViewBuilder
    private var status: some View {
        switch projectStore.state {
        case .idle, .loading:
            SwitcherStatusBlock(message: String(localized: "Loading projects…"), isLoading: true)
        case .signedOut:
            VStack(alignment: .leading, spacing: 8) {
                SwitcherStatusBlock(message: String(localized: "Signed out"))
                signedInAction
            }
        case let .error(message):
            VStack(alignment: .leading, spacing: 8) {
                SwitcherStatusBlock(message: message)
                retryAction
            }
        case .loaded:
            VStack(alignment: .leading, spacing: 8) {
                SwitcherStatusBlock(message: String(localized: "This account has no projects yet."))
                retryAction
            }
        }
    }

    private var retryAction: some View {
        SwitcherActionRow(title: "Refresh projects",
                          systemImage: "arrow.clockwise",
                          isEnabled: projectStore.state != .loading,
                          action: retry)
            .padding(.horizontal, 2)
    }

    private var signedInAction: some View {
        SwitcherActionRow(title: "Sign in",
                          systemImage: "person.crop.circle.badge.checkmark",
                          isEnabled: !accountStore.isSigningIn,
                          action: signIn)
            .padding(.horizontal, 2)
    }

    // MARK: - Actions

    @ViewBuilder
    private var actions: some View {
        if showsManageAction {
            SwitcherActionRow(title: "Manage projects…",
                              systemImage: "slider.horizontal.3",
                              action: openProjectPage)
        }
        SwitcherActionRow(title: "Open in Memory web",
                          systemImage: "safari",
                          isEnabled: activeProjectWebURL != nil,
                          action: openWebProject)
        SwitcherActionRow(title: "Refresh projects",
                          systemImage: "arrow.clockwise",
                          isEnabled: projectStore.state != .loading && !accountStore.isSigningIn,
                          action: retry)
    }

    // MARK: - Keyboard

    private func move(_ direction: MoveCommandDirection) {
        let ids = sections.flatMap(\.rows).map(\.id)
        guard !ids.isEmpty else { return }
        switch direction {
        case .down:
            guard let current = highlightedID, let index = ids.firstIndex(of: current) else {
                highlightedID = ids.first
                return
            }
            highlightedID = ids[min(index + 1, ids.count - 1)]
        case .up:
            guard let current = highlightedID, let index = ids.firstIndex(of: current) else {
                highlightedID = ids.last
                return
            }
            highlightedID = ids[max(index - 1, 0)]
        default:
            break
        }
    }

    // MARK: - Derived

    private func displayName(_ project: ProjectInfo) -> String {
        let name = (project.name ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return name.isEmpty ? String(localized: "Untitled project") : name
    }

    private var activeProjectWebURL: URL? {
        guard let project = projectStore.activeProject else { return nil }
        return accountStore.activeAccount?.environment?
            .projectWebURL(orgID: project.orgID, projectID: project.id)
    }

    // MARK: - Actions

    private func switchProject(to id: String) {
        onDismiss?()
        // No "already active" short-circuit: re-picking the active project is a
        // refresh (it re-runs `projects use` for a connected project), which is
        // covered by `ProjectStoreTests`.
        Task {
            let token = await (try? accountStore.currentAccessToken()) ?? ""
            await projectStore.selectProject(id, accessToken: token)
            // Record only a switch that actually landed: a failed switch leaves
            // the active project unchanged and must not enter the Recent list.
            if case .error = projectStore.state {
                return
            }
            recentProjects.record(id)
        }
    }

    private func retry() {
        onDismiss?()
        Task {
            // Project listing runs through the CLI; the app token is optional
            // (`loadProjects` owns auth classification and repair).
            let token = await (try? accountStore.currentAccessToken()) ?? ""
            await projectStore.loadProjects(accessToken: token)
        }
    }

    private func signIn() {
        onDismiss?()
        Task {
            let environment = accountStore.activeEnvironment ?? .prod
            guard await (try? accountStore.signIn(environment: environment)) != nil else {
                return
            }
            let token = await (try? accountStore.currentAccessToken()) ?? ""
            await projectStore.loadProjects(accessToken: token)
        }
    }

    private func openProjectPage() {
        onDismiss?()
        appState.selectedSidebarItem = .project
        WindowRouter.shared.requestMainWindow()
        NSApp.activate(ignoringOtherApps: true)
    }

    private func openWebProject() {
        guard let url = activeProjectWebURL else { return }
        onDismiss?()
        NSWorkspace.shared.open(url)
    }
}
