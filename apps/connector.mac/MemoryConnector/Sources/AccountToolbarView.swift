import AppKit
import SwiftUI

/// Top-right account control in the window toolbar.
///
/// Effectively signed in → the avatar opens a context panel: the identity block
/// (avatar, display name, email, environment), every signed-in account as a
/// row, then Add account / Manage accounts / Sign out. Signed out → a bordered
/// "Sign in" button that starts Production sign-in directly; no environment
/// choice and no stale account rows are shown.
///
/// The trigger is the avatar plus a disclosure chevron. Two affordances matter
/// here: the avatar says *which* account is active without opening anything, and
/// the chevron says the control opens something. The previous version hid the
/// menu indicator, so a borderless avatar gave no hint that it was a control.
///
/// The panel is `AccountSwitcherPanel`, a real view in a `.popover` — a `Menu`
/// flattens rows to `NSMenuItem`s, which is why the account list read as a bare
/// column of email addresses with no padding and no avatar.
///
/// Gating uses `AccountStore.isEffectivelySignedIn` (active account AND a
/// valid connector CLI session), never the bare index lookup, so the control
/// can never say "Signed in" while the session is gone.
struct AccountToolbarView: View {
    @SwiftUI.Environment(AccountStore.self) private var accountStore
    @SwiftUI.Environment(ProjectStore.self) private var projectStore
    @SwiftUI.Environment(AppState.self) private var appState

    @State private var isHoveringTrigger = false
    @State private var showsPanel = false
    @State private var accountToSignOut: Account?

    var body: some View {
        Group {
            if accountStore.isEffectivelySignedIn {
                accountButton
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

    private var accountButton: some View {
        Button {
            showsPanel.toggle()
        } label: {
            avatarTrigger
        }
        .buttonStyle(.borderless)
        .accessibilityLabel(Text(AccessibleNames.account))
        .accessibilityValue(accessibilityValue)
        .help("Account")
        .disabled(accountStore.isSigningIn)
        .popover(isPresented: $showsPanel, arrowEdge: .bottom) {
            AccountSwitcherPanel(
                onDismiss: { showsPanel = false },
                onAddAccount: { signIn(signInEnvironment) },
                onManageAccounts: manageAccounts,
                onSignOut: { account in accountToSignOut = account }
            )
            .padding(10)
            .frame(width: 300)
        }
    }

    /// Avatar + disclosure chevron. The hover ring stands in for the browser's
    /// `hover:ring-1`, so the control reads as clickable before it is opened.
    private var avatarTrigger: some View {
        HStack(spacing: 3) {
            MemoryAvatar(initials: accountStore.activeAccount?.initials ?? "",
                         size: 22,
                         cacheKey: accountStore.activeAccount?.email)
                .overlay(
                    Circle()
                        .strokeBorder(.primary.opacity(isHoveringTrigger ? 0.28 : 0), lineWidth: 1)
                )
            Image(systemName: "chevron.down")
                .font(.system(size: 8, weight: .semibold))
                .foregroundStyle(.secondary)
        }
        .padding(.horizontal, 4)
        .padding(.vertical, 3)
        .contentShape(Rectangle())
        .onHover { isHoveringTrigger = $0 }
    }

    /// VoiceOver value: the identity the control switches, not just "Account".
    private var accessibilityValue: Text {
        guard let account = accountStore.activeAccount else {
            return Text("Signed out")
        }
        var parts = [account.switcherTitle]
        if account.environmentLabel != account.switcherTitle {
            parts.append(account.environmentLabel)
        }
        return Text(verbatim: parts.joined(separator: ", "))
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

    private func signOut(_ account: Account) {
        Task { await accountStore.signOut(accountID: account.id) }
    }

    private func manageAccounts() {
        appState.selectedSidebarItem = .project
        WindowRouter.shared.requestMainWindow()
        NSApp.activate(ignoringOtherApps: true)
    }
}
