import SwiftUI

/// Project switcher at the leading edge of the window toolbar: a plain,
/// single-line menu showing the active project name with a small chevron (a
/// dropdown, not a bordered button). Mirrors `ProjectStore` state: signed-out,
/// loading, empty, error, and ready (checkmark on the active project).
struct ProjectSwitcherView: View {
    @EnvironmentObject private var accountStore: AccountStore
    @EnvironmentObject private var projectStore: ProjectStore

    @State private var switching: String?

    var body: some View {
        content
            // Self-heal: if bootstrap ran before the session was restored (or
            // failed), load once when the picker appears with no projects.
            .task { await ensureProjectsLoaded() }
    }

    @ViewBuilder
    private var content: some View {
        if !accountStore.isEffectivelySignedIn {
            statusLabel(icon: "person.crop.circle.badge.questionmark",
                        text: "Sign in to switch projects")
        } else if !projectStore.projects.isEmpty {
            projectsMenu
        } else {
            switch projectStore.state {
            case .idle, .loading:
                HStack(spacing: 6) {
                    ProgressView().controlSize(.small)
                    Text("Loading projects…")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            case .signedOut:
                signedOutMenu
            case .error:
                errorMenu
            case .loaded:
                statusLabel(icon: "folder", text: "No projects")
            }
        }
    }

    private func ensureProjectsLoaded() async {
        guard accountStore.isEffectivelySignedIn,
              projectStore.projects.isEmpty,
              projectStore.state != .loading else { return }
        switch projectStore.state {
        case .idle, .error, .signedOut:
            // `.signedOut` retries through `loadProjects`, which itself attempts
            // the session repair hook before falling back to the signed-out UI.
            let token = (try? await accountStore.currentAccessToken()) ?? ""
            await projectStore.loadProjects(accessToken: token)
        case .loading, .loaded:
            return
        }
    }

    // MARK: - Ready

    private var projectsMenu: some View {
        Menu {
            // Every project lives inside a Section with a guaranteed non-empty
            // header ("Other" for unknown orgs) so the whole list renders
            // uniformly indented under its organisation.
            ForEach(projectStore.projectsByOrganization) { group in
                Section {
                    ForEach(group.projects, id: \.id) { project in
                        Button {
                            switchProject(to: project.id)
                        } label: {
                            // Fixed-width leading slot keeps names aligned
                            // whether or not the row is the active project.
                            // A `Menu` drops `.opacity` on its item labels, so
                            // the glyph is included only for the active project
                            // rather than hidden with opacity.
                            HStack(spacing: 6) {
                                if project.id == projectStore.activeProjectID {
                                    Image(systemName: "checkmark")
                                        .font(.system(size: 11, weight: .bold))
                                        .frame(width: 12, alignment: .center)
                                } else {
                                    Color.clear.frame(width: 12)
                                }
                                Text(project.name ?? "Untitled project")
                                if projectStore.isConnected(project.id) {
                                    Image(systemName: "bolt.fill")
                                        .font(.system(size: 9, weight: .bold))
                                        .foregroundStyle(.green)
                                        .help("Connected — serving local tools")
                                }
                            }
                        }
                    }
                } header: {
                    // Never tail-truncate the organisation name.
                    Text(group.sectionTitle)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            Divider()
            Text("Selecting a project views it; connect to serve its tools")
        } label: {
            switcherLabel {
                Image(systemName: hasError ? "exclamationmark.triangle" : "shippingbox")
                    .foregroundStyle(hasError ? Color.orange : Color.secondary)
                Text(projectStore.activeProjectName ?? "Select project")
                    .fontWeight(.semibold)
                    .lineLimit(1)
                if let activeID = projectStore.activeProjectID,
                   projectStore.isConnected(activeID) {
                    Image(systemName: "bolt.fill")
                        .font(.system(size: 9, weight: .bold))
                        .foregroundStyle(.green)
                        .help("Connected — serving local tools")
                }
                if switching != nil {
                    ProgressView().controlSize(.small)
                } else {
                    chevron
                }
            }
        }
        .menuStyle(.borderlessButton)
        .menuIndicator(.hidden)
        .fixedSize()
        .disabled(switching != nil)
        .help(hasError ? switchErrorMessage : "Switch project")
    }

    // MARK: - Error / empty

    private var errorMenu: some View {
        Menu {
            Button("Retry") { retry() }
        } label: {
            switcherLabel {
                Image(systemName: "exclamationmark.triangle")
                    .foregroundStyle(.orange)
                Text("Couldn't load projects")
                    .lineLimit(1)
                chevron
            }
        }
        .menuStyle(.borderlessButton)
        .menuIndicator(.hidden)
        .fixedSize()
        .help(switchErrorMessage)
    }

    private var signedOutMenu: some View {
        Menu {
            Button("Sign in") { signIn() }
            Button("Retry") { retry() }
        } label: {
            switcherLabel {
                Image(systemName: "person.crop.circle.badge.questionmark")
                    .foregroundStyle(.secondary)
                Text("Signed out")
                    .lineLimit(1)
                chevron
            }
        }
        .menuStyle(.borderlessButton)
        .menuIndicator(.hidden)
        .fixedSize()
        .help("Signed out — sign in to load projects")
    }

    private func statusLabel(icon: String, text: String) -> some View {
        switcherLabel {
            Image(systemName: icon)
                .foregroundStyle(.secondary)
            Text(text)
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
        .help(text)
    }

    /// Borderless, single-line chrome: just spacing + a small chevron, so it
    /// reads as a dropdown rather than a second toolbar button.
    private func switcherLabel<Content: View>(@ViewBuilder content: () -> Content) -> some View {
        HStack(spacing: 5) {
            content()
        }
        .font(.subheadline)
    }

    private var chevron: some View {
        Image(systemName: "chevron.down")
            .font(.system(size: 9, weight: .semibold))
            .foregroundStyle(.secondary)
    }

    // MARK: - Derived

    private var hasError: Bool {
        if case .error = projectStore.state { return true }
        return false
    }

    private var switchErrorMessage: String {
        if case .error(let message) = projectStore.state { return message }
        return "Switch project"
    }

    // MARK: - Actions

    private func switchProject(to id: String) {
        guard !id.isEmpty, switching == nil else { return }
        switching = id
        Task {
            defer { switching = nil }
            do {
                let token = try await accountStore.currentAccessToken()
                await projectStore.selectProject(id, accessToken: token)
            } catch {
                // ProjectStore surfaces its own failures via `state`; a thrown
                // token error leaves the previous state, shown by the label.
            }
        }
    }

    private func retry() {
        Task {
            // Project listing runs through the CLI; the app token is optional.
            // `loadProjects` owns the auth classification and repair.
            let token = (try? await accountStore.currentAccessToken()) ?? ""
            await projectStore.loadProjects(accessToken: token)
        }
    }

    /// Re-authenticates the active account (or Prod when none is active) and
    /// reloads projects through the connector CLI.
    private func signIn() {
        Task {
            let environment = accountStore.activeEnvironment ?? .prod
            guard (try? await accountStore.signIn(environment: environment)) != nil else {
                return
            }
            let token = (try? await accountStore.currentAccessToken()) ?? ""
            await projectStore.loadProjects(accessToken: token)
        }
    }
}
