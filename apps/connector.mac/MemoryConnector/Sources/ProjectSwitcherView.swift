import SwiftUI

/// Project switcher at the leading edge of the window toolbar: a plain,
/// single-line menu showing the active project name with a small chevron (a
/// dropdown, not a bordered button). Mirrors `ProjectStore` state: signed-out,
/// loading, empty, error, and ready (checkmark on the active project).
struct ProjectSwitcherView: View {
    @SwiftUI.Environment(AccountStore.self) private var accountStore
    @SwiftUI.Environment(ProjectStore.self) private var projectStore

    @State private var switching: String?

    var body: some View {
        content
            .accessibilityIdentifier("toolbar.project")
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
                // Same horizontal rhythm as `switcherLabel` so the loading row
                // does not shift when it becomes the project dropdown.
                .padding(.horizontal, 10)
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
            let token = await (try? accountStore.currentAccessToken()) ?? ""
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
                                project.name.map { Text($0) } ?? Text("Untitled project")
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
                activeProjectLabel
                    .fontWeight(.semibold)
                    .lineLimit(1)
                if let activeID = projectStore.activeProjectID,
                   projectStore.isConnected(activeID)
                {
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
        .menuStyle(.button)
        .buttonStyle(.borderless)
        .menuIndicator(.hidden)
        .fixedSize()
        .disabled(switching != nil)
        .help(switchHelp)
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
        .menuStyle(.button)
        .buttonStyle(.borderless)
        .menuIndicator(.hidden)
        .fixedSize()
        .help(switchHelp)
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
        .menuStyle(.button)
        .buttonStyle(.borderless)
        .menuIndicator(.hidden)
        .fixedSize()
        .help("Signed out — sign in to load projects")
    }

    private func statusLabel(icon: String, text: LocalizedStringResource) -> some View {
        switcherLabel {
            Image(systemName: icon)
                .foregroundStyle(.secondary)
            Text(text)
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
        .help(Text(text))
    }

    /// Borderless, single-line chrome: spacing + a small chevron and horizontal
    /// padding, so it reads as a dropdown rather than a second toolbar button.
    /// The padding matches the account control next door (10pt) so the two
    /// trailing toolbar items read as a pair; it is the single chrome entry
    /// point, so every label state gets the same left/right space.
    private func switcherLabel(@ViewBuilder content: () -> some View) -> some View {
        HStack(spacing: 5) {
            content()
        }
        .font(.subheadline)
        .padding(.horizontal, 10)
    }

    private var chevron: some View {
        Image(systemName: "chevron.down")
            .font(.system(size: 9, weight: .semibold))
            .foregroundStyle(.secondary)
    }

    // MARK: - Derived

    /// Active project name (runtime data), or a localized placeholder when none
    /// is selected. Returning `Text` keeps the placeholder on the localizable
    /// path instead of folding it into a verbatim `String`.
    private var activeProjectLabel: Text {
        projectStore.activeProjectName.map { Text($0) } ?? Text("Select project")
    }

    private var hasError: Bool {
        if case .error = projectStore.state {
            return true
        }
        return false
    }

    /// Tooltip for the switcher label: the server's error text when the load
    /// failed (data, shown verbatim), otherwise the localized action label.
    private var switchHelp: Text {
        if case let .error(message) = projectStore.state {
            return Text(verbatim: message)
        }
        return Text("Switch project")
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
            let token = await (try? accountStore.currentAccessToken()) ?? ""
            await projectStore.loadProjects(accessToken: token)
        }
    }

    /// Re-authenticates the active account (or Prod when none is active) and
    /// reloads projects through the connector CLI.
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
}
