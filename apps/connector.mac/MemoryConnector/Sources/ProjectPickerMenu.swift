import AppKit
import SwiftUI

/// The project list shared by the toolbar switcher and the Project page card.
///
/// Every selectable row is a `Toggle` bound to "this project is active". In a
/// `Menu`, SwiftUI renders a toggle with the system checkmark in the menu's own
/// leading gutter and exposes a proper selected state to VoiceOver — which is
/// exactly what a custom `HStack` row cannot do: a `Menu` flattens its item
/// labels into `NSMenuItem`s, drops rendering modifiers, and leaves glyphs
/// flush against the row edge (the old design worked around that with
/// `Color.clear.frame(width: 12)` spacers and 9pt hand-sized symbols).
///
/// Structure mirrors the web console's switcher: a "Recent" shortcut section
/// once the list is long enough to need one, projects grouped under their
/// organization, then real actions instead of a disabled hint row. The connect
/// state moved out of the rows into its own section because a menu row has one
/// icon slot and the selection checkmark already owns it.
struct ProjectPickerMenu: View {
    @SwiftUI.Environment(AccountStore.self) private var accountStore
    @SwiftUI.Environment(ProjectStore.self) private var projectStore
    @SwiftUI.Environment(RecentProjectsStore.self) private var recentProjects
    @SwiftUI.Environment(AppState.self) private var appState

    /// The web console only shows its Recent section beyond this many projects;
    /// the connector keeps the same threshold so the two switchers read alike.
    static let recentSectionThreshold = 10

    var body: some View {
        if !accountStore.isEffectivelySignedIn {
            Button("Sign in") { signIn() }
            Button("Retry") { retry() }
        } else if projectStore.projects.isEmpty {
            emptyState
        } else {
            if showRecentSection {
                Section("Recent") {
                    ForEach(recentProjectRows, id: \.id) { project in
                        row(project)
                    }
                }
            }
            if showConnectedSection {
                Section("Connected") {
                    ForEach(projectStore.connectedProjects, id: \.id) { project in
                        row(project)
                    }
                }
            }
            ForEach(projectStore.projectsByOrganization) { group in
                Section {
                    ForEach(group.projects, id: \.id) { project in
                        row(project)
                    }
                } header: {
                    // The group title is runtime data (or the "Other"
                    // fallback), never a localization key.
                    Text(verbatim: group.sectionTitle)
                }
            }
            Divider()
            actions
        }
    }

    // MARK: - Rows

    /// One project row. `Toggle` gives the native checkmark, the reserved
    /// leading gutter, type-ahead selection and an accessible "selected"
    /// state. A click on the ACTIVE row must still reach `selectProject`: for a
    /// connected project that is the documented "refresh/reassert" path
    /// (`ProjectStore.selectProject` re-runs `projects use` and restarts the
    /// child), and a menu toggle reports the click as `false` — so the setter
    /// acts on either value and the row is a pick-one, not a real on/off.
    private func row(_ project: ProjectInfo) -> some View {
        Toggle(isOn: Binding(
            get: { project.id == projectStore.activeProjectID },
            set: { _ in
                switchProject(to: project.id)
            }
        )) {
            projectLabel(project)
        }
        .disabled(projectStore.state == .loading)
    }

    private func projectLabel(_ project: ProjectInfo) -> Text {
        let trimmed = project.name?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return trimmed.isEmpty ? Text("Untitled project") : Text(verbatim: trimmed)
    }

    // MARK: - Empty / error states

    @ViewBuilder
    private var emptyState: some View {
        switch projectStore.state {
        case .idle, .loading:
            Text("Loading projects…")
        case .signedOut:
            Button("Sign in") { signIn() }
            Button("Retry") { retry() }
        case let .error(message):
            Text(verbatim: message)
            Button("Retry") { retry() }
        case .loaded:
            Text("No projects")
            // The footer stays reachable with an empty list: "Manage projects…"
            // opens the page where the account's projects are listed, and a
            // refresh re-checks after a project was created elsewhere.
            Divider()
            actions
        }
    }

    // MARK: - Footer actions

    @ViewBuilder
    private var actions: some View {
        Button {
            openProjectPage()
        } label: {
            Label("Manage projects…", systemImage: "slider.horizontal.3")
        }
        Button {
            openWebProject()
        } label: {
            Label("Open in Memory web", systemImage: "safari")
        }
        .disabled(activeProjectWebURL == nil)
        Button {
            retry()
        } label: {
            Label("Refresh projects", systemImage: "arrow.clockwise")
        }
        .disabled(projectStore.state == .loading || accountStore.isSigningIn)
    }

    // MARK: - Derived

    private var showRecentSection: Bool {
        projectStore.projects.count > Self.recentSectionThreshold && !recentProjectRows.isEmpty
    }

    /// Recent projects resolved against the loaded list, newest first.
    private var recentProjectRows: [ProjectInfo] {
        recentProjects.resolved(in: projectStore.projects)
    }

    private var showConnectedSection: Bool {
        projectStore.projects.count > 1 && !projectStore.connectedProjects.isEmpty
    }

    /// Web console URL for the active project, when the org is resolvable.
    private var activeProjectWebURL: URL? {
        guard let project = projectStore.activeProject else { return nil }
        return activeEnvironment?.projectWebURL(orgID: project.orgID, projectID: project.id)
    }

    private var activeEnvironment: Environment? {
        accountStore.activeAccount?.environment
    }

    // MARK: - Actions

    private func switchProject(to id: String) {
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
        Task {
            // Project listing runs through the CLI; the app token is optional
            // (`loadProjects` owns auth classification and repair).
            let token = await (try? accountStore.currentAccessToken()) ?? ""
            await projectStore.loadProjects(accessToken: token)
        }
    }

    private func signIn() {
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
        appState.selectedSidebarItem = .project
        WindowRouter.shared.requestMainWindow()
        NSApp.activate(ignoringOtherApps: true)
    }

    private func openWebProject() {
        guard let url = activeProjectWebURL else { return }
        NSWorkspace.shared.open(url)
    }
}
