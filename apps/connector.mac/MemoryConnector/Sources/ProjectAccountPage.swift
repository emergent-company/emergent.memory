import SwiftUI

/// Project & Account page. Both the account card and the project card prefer
/// the signed-in account; when nothing is signed in they show a clear
/// sign-in prompt. The project card hosts the project switcher when signed in
/// (names lead; ids stay behind the copy control).
struct ProjectAccountPage: View {
    @SwiftUI.Environment(ConnectorSettings.self) private var settings
    @SwiftUI.Environment(AccountStore.self) private var accountStore
    @SwiftUI.Environment(ProjectStore.self) private var projectStore
    @SwiftUI.Environment(IdentityStore.self) private var identity
    @SwiftUI.Environment(AppState.self) private var appState

    @State private var pageError: String?
    @State private var accountToSignOut: Account?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                if let pageError {
                    ErrorBanner(message: pageError)
                }
                if accountStore.accounts.isEmpty {
                    // Exactly one sign-in prompt on the page while signed out.
                    SignInCard { appState.selectedSidebarItem = .connection }
                } else {
                    // List stored accounts even when the CLI session is invalid
                    // so a stored Dev account can be re-authenticated (not just
                    // a generic Production sign-in).
                    AccountsCard(
                        accounts: accountStore.accounts,
                        activeAccountID: accountStore.activeAccountID,
                        isSigningIn: accountStore.isSigningIn,
                        needsReauthentication: { accountStore.needsReauthentication($0) },
                        onSignOut: { accountToSignOut = $0 },
                        onAddAccount: { addAccount(addAccountEnvironment) },
                        onSignInAgain: { reauthenticate($0) }
                    )
                    if accountStore.isEffectivelySignedIn {
                        ProjectCard(
                            projects: projectStore.projects,
                            state: projectStore.state,
                            activeProject: activeProject,
                            activeProjectName: projectStore.activeProjectName,
                            activeProjectID: projectStore.activeProjectID,
                            snapshot: currentSnapshot,
                            isSigningIn: accountStore.isSigningIn,
                            onTryAgain: { refreshAll() },
                            onSignIn: { signInFromSignedOutState() }
                        )
                        ProjectConnectionControl()
                    }
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
        .onChange(of: settings.serverURL) { _, _ in refreshAll() }
        .onChange(of: accountStore.activeAccountID) { _, activeID in
            if activeID != nil {
                Task { await loadProjects() }
            } else {
                pageError = nil
            }
        }
        .confirmationDialog(Text(SignOutConfirmation.title),
                            isPresented: Binding(get: { accountToSignOut != nil },
                                                 set: {
                                                     if !$0 {
                                                         accountToSignOut = nil
                                                     }
                                                 }),
                            titleVisibility: .visible,
                            presenting: accountToSignOut)
        { account in
            Button(role: .destructive) {
                signOut(account)
            } label: {
                Text(SignOutConfirmation.confirmButtonTitle(displayTitle: account.displayTitle))
            }
            Button("Cancel", role: .cancel) {}
        } message: { account in
            Text(SignOutConfirmation.message(displayTitle: account.displayTitle))
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
            if AuthFailure.looksLikeAuthFailure(error) || !accountStore.isEffectivelySignedIn {
                // An expected auth/session failure (or a now-signed-out account)
                // is not an error — suppress the raw banner.
                pageError = nil
            } else {
                pageError = error.localizedDescription
            }
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

    private var activeProject: ProjectInfo? {
        projectStore.projects.first { $0.id == projectStore.activeProjectID }
    }

    private var currentSnapshot: IdentitySnapshot? {
        if case let .loaded(snapshot) = identity.state {
            return snapshot
        }
        return nil
    }

    /// The environment the "Add account" action targets, from the shared
    /// per-surface policy.
    private var addAccountEnvironment: Environment {
        Environment.signInEnvironments(for: .projectAccount).first ?? Environment.primary
    }

    private func addAccount(_ environment: Environment) {
        Task { _ = try? await accountStore.signIn(environment: environment) }
    }

    /// Re-authenticates a stored account that needs sign-in again, targeting
    /// the account's OWN environment (a Dev account stays Dev). Then reloads
    /// projects; an explicit failure surfaces its own error.
    private func reauthenticate(_ account: Account) {
        guard let environment = account.environment else { return }
        Task {
            do {
                _ = try await accountStore.signIn(environment: environment)
                await loadProjects()
            } catch {
                pageError = error.localizedDescription
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
}
