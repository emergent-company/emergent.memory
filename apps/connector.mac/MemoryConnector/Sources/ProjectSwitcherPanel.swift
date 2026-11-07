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

    /// Section-scoped row key, NOT the project id: the Recent section repeats
    /// projects that also appear under their organisation, so a project id
    /// identifies up to two rendered rows.
    @State private var highlightedKey: String?
    @FocusState private var isFocused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if showsHeader {
                header
            }
            content
            actions
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .focusable()
        .focused($isFocused)
        .focusEffectDisabled()
        .onMoveCommand(perform: move)
        .onKeyPress(.return) {
            guard let key = highlightedKey,
                  let row = navigationRows.first(where: { $0.key == key })
            else { return .ignored }
            switchProject(to: row.projectID)
            return .handled
        }
        .onAppear(perform: prepareKeyboard)
    }

    /// Signed-out is decided BEFORE the list is considered: a failed load can
    /// leave a stale populated list behind (`.signedOut` is set without clearing
    /// `projects`), and painting those rows as enabled next to a trigger that
    /// says "Sign in" is worse than showing the sign-in block.
    @ViewBuilder
    private var content: some View {
        if !accountStore.isEffectivelySignedIn || projectStore.state == .signedOut {
            signedOutBlock
        } else if projectStore.projects.isEmpty {
            status
        } else {
            rows
            SwitcherDivider()
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
                    isActive: row.isActive,
                    isConnected: row.isConnected,
                    isHighlighted: highlightedKey == rowKey(section: section, row: row),
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
            // Unreachable from `content`, which handles signed-out first; kept so
            // the switch stays exhaustive over the store's state.
            signedOutBlock
        case let .error(message):
            // The action footer already carries "Refresh projects" — no second
            // copy next to the error text.
            SwitcherStatusBlock(message: message)
        case .loaded:
            SwitcherStatusBlock(message: String(localized: "This account has no projects yet."))
        }
    }

    private var signedOutBlock: some View {
        VStack(alignment: .leading, spacing: 8) {
            SwitcherStatusBlock(message: String(localized: "Sign in to switch projects."))
            SwitcherActionRow(title: "Sign in",
                              systemImage: "person.crop.circle.badge.checkmark",
                              isEnabled: !accountStore.isSigningIn,
                              action: signIn)
                .padding(.horizontal, 2)
        }
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
                          isEnabled: projectStore.state != .loading,
                          action: retry)
    }

    // MARK: - Keyboard

    private struct NavigationRow {
        let key: String
        let projectID: String
    }

    /// Every rendered row, in render order, keyed by section so a project that
    /// appears in both Recent and its organisation is two distinct stops.
    private var navigationRows: [NavigationRow] {
        sections.flatMap { section in
            section.rows.map {
                NavigationRow(key: rowKey(section: section, row: $0), projectID: $0.id)
            }
        }
    }

    private func rowKey(section: ProjectSwitcherSection, row: ProjectSwitcherRow) -> String {
        "\(section.id)/\(row.id)"
    }

    /// Seeds the highlight on the active project (its first occurrence, i.e. the
    /// Recent row) and takes focus. Deferred by one runloop turn: during
    /// `onAppear` the popover window is not key yet and a synchronous focus
    /// assignment is silently dropped, which would leave arrow keys and Return
    /// dead with no visible symptom. Never takes focus when embedded, where the
    /// page owns the keyboard.
    private func prepareKeyboard() {
        guard onDismiss != nil else { return }
        highlightedKey = navigationRows.first { $0.projectID == projectStore.activeProjectID }?.key
        DispatchQueue.main.async { isFocused = true }
    }

    private func move(_ direction: MoveCommandDirection) {
        let keys = navigationRows.map(\.key)
        guard !keys.isEmpty else { return }
        switch direction {
        case .down:
            guard let current = highlightedKey, let index = keys.firstIndex(of: current) else {
                highlightedKey = keys.first
                return
            }
            highlightedKey = keys[min(index + 1, keys.count - 1)]
        case .up:
            guard let current = highlightedKey, let index = keys.firstIndex(of: current) else {
                highlightedKey = keys.last
                return
            }
            highlightedKey = keys[max(index - 1, 0)]
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
