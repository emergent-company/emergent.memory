import AppKit
import SwiftUI

/// Top-right account control in the window toolbar.
///
/// Effectively signed in → the person icon opens the account switcher: every
/// signed-in account listed by EMAIL (environment badge, active checkmark)
/// with switching, an "Add account" action (signs in to Production), and
/// "Manage accounts…" (the Project & Account page). Not effectively signed in
/// → a padded "Sign in" button that starts Production sign-in directly; no
/// environment choice and no stale account rows are shown.
///
/// Gating uses `AccountStore.isEffectivelySignedIn` (active account AND a
/// valid connector CLI session), never the bare index lookup, so the control
/// can never say "Signed in" while the session is gone.
struct AccountToolbarView: View {
    @EnvironmentObject private var accountStore: AccountStore
    @EnvironmentObject private var projectStore: ProjectStore
    @EnvironmentObject private var appState: AppState

    var body: some View {
        if accountStore.isEffectivelySignedIn {
            accountMenu
        } else {
            signInButton
        }
    }

    // MARK: - Signed in

    private var accountMenu: some View {
        Menu {
            Section("Accounts") {
                ForEach(accountStore.accounts) { account in
                    Button {
                        switchAccount(account.id)
                    } label: {
                        AccountRowLabel(account: account,
                                        isActive: account.id == accountStore.activeAccountID)
                    }
                }
            }
            Section {
                Button {
                    signIn(signInEnvironment)
                } label: {
                    Label("Add account", systemImage: "person.badge.plus")
                }
                Button {
                    manageAccounts()
                } label: {
                    Label("Manage accounts…", systemImage: "gearshape")
                }
            }
        } label: {
            Image(systemName: "person.crop.circle")
                .imageScale(.large)
        }
        .menuStyle(.borderlessButton)
        .fixedSize()
        .disabled(accountStore.isSigningIn)
        .help("Account")
    }

    // MARK: - Signed out

    /// Padded "Sign in" button. Starts Production sign-in in one click — no
    /// environment choice, so the very first sign-in never looks like a
    /// product decision.
    private var signInButton: some View {
        Button {
            signIn(signInEnvironment)
        } label: {
            HStack(spacing: 6) {
                if accountStore.isSigningIn {
                    ProgressView().controlSize(.small)
                }
                Text(accountStore.isSigningIn ? "Signing in…" : "Sign in")
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 4)
            .background(
                RoundedRectangle(cornerRadius: 6, style: .continuous)
                    .fill(.quaternary.opacity(0.5))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 6, style: .continuous)
                    .strokeBorder(.quaternary)
            )
        }
        .buttonStyle(.plain)
        .fixedSize()
        .disabled(accountStore.isSigningIn)
        .help("Sign in with Memory")
    }

    // MARK: - Actions

    /// The environment this control's sign-in action targets, from the shared
    /// per-surface policy.
    private var signInEnvironment: Environment {
        Environment.signInEnvironments(for: .windowHeader).first ?? Environment.primary
    }

    private func signIn(_ environment: Environment) {
        Task {
            // The AppEnvironment hook applies the account scope + reloads data
            // for a NEW active account; re-signing in to the same account does
            // not change the id, so reload projects here as well.
            guard (try? await accountStore.signIn(environment: environment)) != nil else { return }
            let token = (try? await accountStore.currentAccessToken()) ?? ""
            await projectStore.loadProjects(accessToken: token)
        }
    }

    private func switchAccount(_ id: String) {
        guard id != accountStore.activeAccountID else { return }
        Task { try? await accountStore.switchTo(accountID: id) }
    }

    private func manageAccounts() {
        appState.selectedSidebarItem = .project
        WindowRouter.shared.requestMainWindow()
        NSApp.activate(ignoringOtherApps: true)
    }
}
