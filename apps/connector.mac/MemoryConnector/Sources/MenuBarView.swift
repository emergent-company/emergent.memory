import AppKit
import SwiftUI

/// Popover content for a LEFT-click on the status item.
///
/// Shows a status summary + tool count, the account switcher (signed in) or a
/// single Production sign-in call (signed out), and the same action set the
/// right-click `NSMenu` exposes (open, check for updates, Settings, sign out,
/// Quit) so both click paths agree. Sign-out is confirmed by the enclosing
/// `StatusItemController`, which owns the shared confirmation.
struct MenuBarView: View {
    @SwiftUI.Environment(AccountStore.self) private var accountStore
    @SwiftUI.Environment(ProjectStore.self) private var projectStore
    @SwiftUI.Environment(EngineManager.self) private var engine
    @SwiftUI.Environment(StatusMonitor.self) private var statusMonitor
    @SwiftUI.Environment(AppState.self) private var appState
    @SwiftUI.Environment(UpdaterModel.self) private var updater

    /// Controller-owned actions, so the popover and the secondary-click menu run
    /// the same code (confirmed sign-out, quit) instead of duplicating it.
    var onSignOut: @MainActor () -> Void = {}
    var onQuit: @MainActor () -> Void = {}

    private var status: AppStatus {
        AppStatus.derive(engine: engine.state, snapshot: statusMonitor.snapshot)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
                .padding(.horizontal, 14)
                .padding(.top, 12)
                .padding(.bottom, 10)

            Divider()

            statusSummary
                .padding(.horizontal, 14)
                .padding(.vertical, 8)

            Divider()

            if accountStore.isEffectivelySignedIn {
                signedIn
            } else {
                signedOut
            }

            Divider()

            actions
        }
        .frame(width: 300)
    }

    // MARK: - Header

    private var header: some View {
        HStack(spacing: 8) {
            Image(systemName: status.symbolName)
                .foregroundStyle(status.color)
            Text("Memory")
                .font(.headline)
            Spacer()
            if let version = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String {
                Text("v\(version)")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
                    .monospacedDigit()
            }
        }
    }

    // MARK: - Status summary

    /// Engine/hub state plus the number of tools the running connector reports.
    private var statusSummary: some View {
        HStack(spacing: 6) {
            Text(status.label)
                .font(.caption)
                .foregroundStyle(.secondary)
            if let count = statusMonitor.snapshot?.toolCount, count > 0 {
                Text("·")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
                Text("\(count) \(count == 1 ? "tool" : "tools") registered")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
        }
    }

    // MARK: - Signed in

    private var signedIn: some View {
        VStack(alignment: .leading, spacing: 12) {
            if let account = accountStore.activeAccount {
                AccountRowLabel(account: account,
                                isActive: true,
                                showsCheckmark: false)
            }

            let others = accountStore.accounts.filter { $0.id != accountStore.activeAccountID }
            if !others.isEmpty {
                Divider()
                Text("Switch account")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                ForEach(others) { account in
                    Button {
                        switchAccount(account.id)
                    } label: {
                        AccountRowLabel(account: account)
                    }
                    .buttonStyle(.plain)
                }
            }

            Divider()

            Button {
                signIn(signInEnvironment)
            } label: {
                Label("Add account", systemImage: "plus")
                    .font(.callout)
            }
            .disabled(accountStore.isSigningIn)

            if accountStore.isSigningIn {
                HStack(spacing: 6) {
                    ProgressView().controlSize(.small)
                    Text("Signing in…")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
    }

    // MARK: - Signed out

    private var signedOut: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Not signed in")
                .font(.callout)
                .foregroundStyle(.secondary)
            Text("Sign in with Memory to connect your account and projects.")
                .font(.caption)
                .foregroundStyle(.secondary)

            Button {
                signIn(signInEnvironment)
            } label: {
                Text("Sign in with Memory")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .disabled(accountStore.isSigningIn)

            if accountStore.isSigningIn {
                HStack(spacing: 6) {
                    ProgressView().controlSize(.small)
                    Text("Signing in…")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .disabled(accountStore.isSigningIn)
    }

    // MARK: - Actions (same set as the secondary-click menu)

    private var actions: some View {
        VStack(alignment: .leading, spacing: 2) {
            actionButton("Open Memory", systemImage: "macwindow") {
                openApplication()
            }
            actionButton("Check for Updates…", systemImage: "arrow.triangle.2.circlepath") {
                updater.checkForUpdates()
            }
            .disabled(!updater.canCheckForUpdates)
            actionButton("Settings…", systemImage: "gearshape") {
                openSettings()
            }
            if accountStore.isEffectivelySignedIn {
                actionButton("Sign out", systemImage: "rectangle.portrait.and.arrow.right") {
                    onSignOut()
                }
            }
            Divider()
                .padding(.vertical, 4)
            actionButton("Quit Memory", systemImage: "power") {
                onQuit()
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
    }

    private func actionButton(_ title: LocalizedStringKey, systemImage: String,
                              action: @escaping () -> Void) -> some View
    {
        Button(action: action) {
            Label(title, systemImage: systemImage)
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(Rectangle())
        }
        .buttonStyle(.borderless)
    }

    // MARK: - Actions helpers

    private var signInEnvironment: Environment {
        Environment.signInEnvironments(for: .menuBar).first ?? Environment.primary
    }

    private func openApplication() {
        WindowRouter.shared.requestMainWindow()
        NSApp.activate(ignoringOtherApps: true)
    }

    private func openSettings() {
        appState.selectedSidebarItem = .connection
        WindowRouter.shared.requestMainWindow()
        NSApp.activate(ignoringOtherApps: true)
    }

    private func signIn(_ environment: Environment) {
        Task {
            guard await (try? accountStore.signIn(environment: environment)) != nil else { return }
            let token = await (try? accountStore.currentAccessToken()) ?? ""
            await projectStore.loadProjects(accessToken: token)
        }
    }

    private func switchAccount(_ id: String) {
        Task { try? await accountStore.switchTo(accountID: id) }
    }
}
