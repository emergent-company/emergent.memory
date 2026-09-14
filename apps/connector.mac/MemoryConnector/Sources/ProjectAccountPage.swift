import SwiftUI

/// Project & Account page. Both the account card and the project card prefer
/// the signed-in account; when nothing is signed in they show a clear
/// sign-in prompt. The project card hosts the project switcher when signed in
/// (names lead; ids stay behind the copy control).
struct ProjectAccountPage: View {
    @EnvironmentObject private var settings: ConnectorSettings
    @EnvironmentObject private var accountStore: AccountStore
    @EnvironmentObject private var projectStore: ProjectStore
    @EnvironmentObject private var identity: IdentityStore
    @EnvironmentObject private var appState: AppState

    @State private var pageError: String?
    @State private var accountToSignOut: Account?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                if let pageError {
                    errorBanner(pageError)
                }
                accountsCard
                projectCard
                if accountStore.isEffectivelySignedIn {
                    ProjectConnectionControl()
                    sessionCard
                }
            }
            .padding(24)
            .frame(maxWidth: 720, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
        .navigationTitle("Project & Account")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button {
                    refreshAll()
                } label: {
                    Label("Refresh", systemImage: "arrow.clockwise")
                }
                .disabled(identity.state.isLoading)
            }
        }
        .task { await initialLoad() }
        .onChange(of: settings.configurationRevision) { _, _ in refreshAll() }
        .onChange(of: accountStore.activeAccountID) { _, activeID in
            if activeID != nil {
                Task { await loadProjects() }
            } else {
                pageError = nil
            }
        }
        .confirmationDialog("Sign out of Memory?",
                            isPresented: Binding(get: { accountToSignOut != nil },
                                                 set: { if !$0 { accountToSignOut = nil } }),
                            titleVisibility: .visible,
                            presenting: accountToSignOut) { account in
            Button("Sign out \(account.displayTitle)", role: .destructive) {
                signOut(account)
            }
            Button("Cancel", role: .cancel) {}
        } message: { account in
            Text("This stops the connector if \(account.displayTitle) is active and clears that account's saved session. Other accounts stay signed in.")
        }
    }

    // MARK: - Loading

    private func initialLoad() async {
        guard accountStore.isEffectivelySignedIn else { return }
        await loadIdentity()
        await loadProjects()
    }

    private func refreshAll() {
        guard accountStore.isEffectivelySignedIn else { return }
        Task {
            await loadIdentity()
            await loadProjects()
        }
    }

    private func loadIdentity() async {
        let accessToken = try? await accountStore.currentAccessToken()
        await identity.load(serverURL: activeServerURL, token: accessToken)
    }

    private func loadProjects() async {
        do {
            let token = try await accountStore.currentAccessToken()
            await projectStore.loadProjects(accessToken: token)
            pageError = nil
        } catch {
            pageError = error.localizedDescription
        }
    }

    private func signOut(_ account: Account) {
        Task {
            await accountStore.signOut(accountID: account.id)
        }
    }

    private var activeServerURL: String {
        accountStore.activeEnvironment?.serverURLString ?? settings.serverURL
    }

    // MARK: - Accounts

    /// Lists EVERY signed-in account (identity + environment badge + active
    /// checkmark) with per-account sign-out and an "Add account" action. Shows
    /// the sign-in prompt when no account is signed in.
    private var accountsCard: some View {
        ConnectorCard(title: "Accounts", systemImage: "person.2.circle") {
            if accountStore.isEffectivelySignedIn {
                accountsList
            } else {
                signInPrompt
            }
        }
    }

    private var accountsList: some View {
        VStack(alignment: .leading, spacing: 14) {
            ForEach(accountStore.accounts) { account in
                accountRow(account)
            }
            Divider()
            addAccountMenu
        }
    }

    private func accountRow(_ account: Account) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                AccountRowLabel(account: account,
                                showsAvatar: true,
                                avatarSize: 40,
                                isActive: account.id == accountStore.activeAccountID)
                if accountStore.needsReauthentication(account.id) {
                    Label("Needs sign-in again", systemImage: "exclamationmark.triangle")
                        .font(.caption)
                        .foregroundStyle(.orange)
                }
            }
            Button(role: .destructive) {
                accountToSignOut = account
            } label: {
                Text("Sign out")
            }
            .controlSize(.small)
            .disabled(accountStore.isSigningIn)
        }
    }

    private var addAccountMenu: some View {
        Menu {
            ForEach(Environment.all) { environment in
                Button {
                    addAccount(environment)
                } label: {
                    Text("Sign in to \(environment.shortLabel)")
                }
            }
        } label: {
            Label("Add account", systemImage: "person.badge.plus")
        }
        .menuStyle(.borderlessButton)
        .fixedSize()
        .disabled(accountStore.isSigningIn)
    }

    private func addAccount(_ environment: Environment) {
        Task { _ = try? await accountStore.signIn(environment: environment) }
    }

    @ViewBuilder
    private var signInPrompt: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Sign in with Memory to connect your account and projects.")
                .font(.callout)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)

            Button {
                appState.selectedSidebarItem = .connection
            } label: {
                Label("Sign in with Memory", systemImage: "person.crop.circle.badge.checkmark")
            }
            .buttonStyle(.borderedProminent)
        }
    }

    // MARK: - Session (active account)

    private var sessionCard: some View {
        ConnectorCard(title: "Session", systemImage: "rectangle.portrait.and.arrow.right") {
            VStack(alignment: .leading, spacing: 10) {
                Text("Managing the active account. Signing it out stops the connector and clears its saved session and project tokens; other accounts stay signed in.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)

                if let account = accountStore.activeAccount {
                    Button(role: .destructive) {
                        accountToSignOut = account
                    } label: {
                        Label("Sign out \(account.displayTitle)",
                              systemImage: "rectangle.portrait.and.arrow.right")
                    }
                }
            }
        }
    }

    // MARK: - Project

    private var projectCard: some View {
        ConnectorCard(title: "Project", systemImage: "building.2") {
            if accountStore.isEffectivelySignedIn {
                signedInProjectSection
            } else {
                signInPrompt
            }
        }
    }

    // MARK: Signed-in project details

    @ViewBuilder
    private var signedInProjectSection: some View {
        if projectStore.projects.isEmpty {
            switch projectStore.state {
            case .idle, .loading:
                ConnectorLoadingRow(caption: "Loading projects…")
            case .signedOut:
                signedOutState
            case .error(let message):
                projectErrorState(message)
            case .loaded:
                emptyProjectsState
            }
        } else {
            VStack(alignment: .leading, spacing: 14) {
                activeProjectDetails
                switchHint

                if case .signedOut = projectStore.state {
                    signedOutState
                } else if case .error(let message) = projectStore.state {
                    inlineError(message)
                }
            }
        }
    }

    private var switchHint: some View {
        HStack(spacing: 6) {
            Image(systemName: "arrow.up.to.line")
                .font(.caption)
            Text("Switch projects from the menu at the top of the window.")
        }
        .font(.caption)
        .foregroundStyle(.secondary)
    }

    private var activeProject: ProjectInfo? {
        projectStore.projects.first { $0.id == projectStore.activeProjectID }
    }

    private var activeProjectDetails: some View {
        let orgName = currentSnapshot?.organizationName
        let orgID = currentSnapshot?.organizationID ?? activeProject?.orgID
        return VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 8) {
                Text(projectStore.activeProjectName ?? activeProject?.name ?? "No project selected")
                    .font(.title3.weight(.semibold))
                    .lineLimit(1)
                    .truncationMode(.tail)
                IdentifierReveal(identifier: projectStore.activeProjectID ?? activeProject?.id ?? "")
                Spacer(minLength: 0)
            }

            if let orgName {
                HStack(spacing: 8) {
                    Image(systemName: "building.2")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Text(orgName)
                        .font(.callout)
                        .foregroundStyle(HierarchicalShapeStyle.secondary)
                        .lineLimit(1)
                        .truncationMode(.tail)
                    IdentifierReveal(identifier: orgID ?? "")
                    Spacer(minLength: 0)
                }
            }
        }
    }

    // MARK: - States

    private var currentSnapshot: IdentitySnapshot? {
        if case .loaded(let snapshot) = identity.state { return snapshot }
        return nil
    }

    private var emptyProjectsState: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("No projects yet")
                .font(.headline)
            Text("Your Memory account doesn't have any projects you can use yet.")
                .font(.callout)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private func projectErrorState(_ message: String) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                Image(systemName: "exclamationmark.triangle")
                    .foregroundStyle(.orange)
                Text("Couldn't load projects")
                    .font(.headline)
            }
            Text(message)
                .font(.callout)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            Button("Try Again") { refreshAll() }
        }
    }

    /// Auth/session failure: the app still has an account, but the connector
    /// CLI has no valid session, so the raw error would be confusing. Offer a
    /// clear re-authentication action instead of an error message.
    private var signedOutState: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 8) {
                Image(systemName: "person.crop.circle.badge.questionmark")
                    .foregroundStyle(.secondary)
                Text("Signed out")
                    .font(.headline)
            }
            Text("Your session expired or was cleared on this Mac. Sign in to load your projects.")
                .font(.callout)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            HStack(spacing: 10) {
                Button {
                    signInFromSignedOutState()
                } label: {
                    if accountStore.isSigningIn {
                        HStack(spacing: 6) {
                            ProgressView().controlSize(.small)
                            Text("Signing in…")
                        }
                    } else {
                        Label("Sign in", systemImage: "person.crop.circle.badge.checkmark")
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(accountStore.isSigningIn)

                Button("Try again") { refreshAll() }
                    .disabled(accountStore.isSigningIn)
            }
        }
    }

    /// The environment to sign in to when re-authenticating the active account:
    /// the active account's own environment, else the environment matching the
    /// saved server URL, else Prod.
    private var signInEnvironment: Environment {
        accountStore.activeEnvironment
            ?? Environment.all.first { $0.serverURLString == settings.serverURL }
            ?? .prod
    }

    private func signInFromSignedOutState() {
        Task {
            do {
                _ = try await accountStore.signIn(environment: signInEnvironment)
                await loadProjects()
            } catch {
                pageError = error.localizedDescription
            }
        }
    }

    private func inlineError(_ message: String) -> some View {
        Label(message, systemImage: "exclamationmark.triangle")
            .font(.caption)
            .foregroundStyle(.orange)
            .fixedSize(horizontal: false, vertical: true)
    }

    private func errorBanner(_ message: String) -> some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.orange)
            Text(message)
                .font(.callout)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 0)
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(Color.orange.opacity(0.1))
        )
    }
}
