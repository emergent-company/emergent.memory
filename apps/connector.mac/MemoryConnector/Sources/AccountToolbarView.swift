import AppKit
import SwiftUI

/// Top-right account control in the window toolbar.
///
/// Effectively signed in → the account avatar opens the account switcher: every
/// signed-in account listed by EMAIL (environment badge when the accounts span
/// more than one environment, active row checked by the menu's own checkmark)
/// with switching, an "Add account" action (signs in to Production),
/// "Manage accounts…" (the Project & Account page) and a confirmed sign-out.
/// Not effectively signed in → a bordered "Sign in" button that starts
/// Production sign-in directly; no environment choice and no stale account rows
/// are shown.
///
/// The trigger is the user's avatar rather than a person glyph: it identifies
/// *which* account is active without opening the menu, and it matches the web
/// console's account menu, which is the same control in the same corner.
///
/// Gating uses `AccountStore.isEffectivelySignedIn` (active account AND a
/// valid connector CLI session), never the bare index lookup, so the control
/// can never say "Signed in" while the session is gone.
struct AccountToolbarView: View {
    @SwiftUI.Environment(AccountStore.self) private var accountStore
    @SwiftUI.Environment(ProjectStore.self) private var projectStore
    @SwiftUI.Environment(AppState.self) private var appState

    @State private var isHoveringTrigger = false
    @State private var accountToSignOut: Account?

    var body: some View {
        Group {
            if accountStore.isEffectivelySignedIn {
                accountMenu
            } else {
                signInButton
            }
        }
        .accessibilityIdentifier("toolbar.account")
        .confirmationDialog(Text(SignOutConfirmation.title),
                            isPresented: Binding(
                                get: { accountToSignOut != nil },
                                set: {
                                    if !$0 {
                                        accountToSignOut = nil
                                    }
                                }
                            ),
                            titleVisibility: .visible,
                            presenting: accountToSignOut)
        { account in
            Button(role: .destructive) {
                signOut(account)
            } label: {
                Text(SignOutConfirmation.confirmButtonTitle(displayTitle: account.displayTitle))
            }
            Button(role: .cancel) {} label: { Text("Cancel") }
        } message: { account in
            Text(SignOutConfirmation.message(displayTitle: account.displayTitle))
        }
    }

    // MARK: - Signed in

    private var accountMenu: some View {
        Menu {
            Section("Accounts") {
                ForEach(accountStore.accounts) { account in
                    Toggle(isOn: Binding(
                        get: { account.id == accountStore.activeAccountID },
                        set: { isOn in
                            if isOn {
                                switchAccount(account.id)
                            }
                        }
                    )) {
                        accountRowLabel(account)
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
            Divider()
            Button(role: .destructive) {
                accountToSignOut = accountStore.activeAccount
            } label: {
                Label("Sign out", systemImage: "rectangle.portrait.and.arrow.right")
            }
        } label: {
            avatarTrigger
        }
        .menuStyle(.button)
        .buttonStyle(.borderless)
        .fixedSize()
        .disabled(accountStore.isSigningIn)
        .accessibilityLabel(Text(AccessibleNames.account))
        .help("Account")
    }

    /// Avatar + hover ring. The ring stands in for the browser's
    /// `hover:ring-1`, so the control reads as clickable without a chevron.
    private var avatarTrigger: some View {
        MemoryAvatar(initials: accountStore.activeAccount?.initials ?? "",
                     size: 22,
                     cacheKey: accountStore.activeAccount?.email)
            .padding(3)
            .overlay(
                Circle()
                    .strokeBorder(.primary.opacity(isHoveringTrigger ? 0.28 : 0), lineWidth: 1)
            )
            .contentShape(Circle())
            .onHover { isHoveringTrigger = $0 }
    }

    /// Email first (never initials), with the environment only when the stored
    /// accounts actually span more than one — otherwise "Prod" on every row is
    /// noise. Both halves are runtime data, so the row is built from verbatim
    /// text rather than a localization key.
    private func accountRowLabel(_ account: Account) -> Text {
        let title = Text(verbatim: account.switcherTitle)
        guard showsEnvironmentLabels else { return title }
        return title
            + Text(verbatim: " · ")
            + Text(verbatim: account.environmentLabel)
    }

    private var showsEnvironmentLabels: Bool {
        Set(accountStore.accounts.map(\.environmentID)).count > 1
    }

    // MARK: - Signed out

    /// Bordered system button — the same chrome as every other primary control,
    /// instead of the hand-drawn rounded rectangle the old control used.
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
        }
        .buttonStyle(.bordered)
        .controlSize(.small)
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
            guard await (try? accountStore.signIn(environment: environment)) != nil else { return }
            let token = await (try? accountStore.currentAccessToken()) ?? ""
            await projectStore.loadProjects(accessToken: token)
        }
    }

    private func switchAccount(_ id: String) {
        guard id != accountStore.activeAccountID else { return }
        Task { try? await accountStore.switchTo(accountID: id) }
    }

    private func signOut(_ account: Account) {
        Task { await accountStore.signOut(accountID: account.id) }
    }

    private func manageAccounts() {
        appState.selectedSidebarItem = .project
        WindowRouter.shared.requestMainWindow()
        NSApp.activate(ignoringOtherApps: true)
    }
}
