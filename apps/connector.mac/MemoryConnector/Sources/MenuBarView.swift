import AppKit
import SwiftUI

/// Popover content for a LEFT-click on the status item.
///
/// Effectively signed in → the active account listed by EMAIL (environment
/// badge) plus a switcher for the other accounts and an "Add account" menu.
/// Not effectively signed in → an explicit Prod/Dev sign-in call to action and
/// no account rows. Settings/Quit live in the right-click menu
/// (`StatusItemController`), not here.
struct MenuBarView: View {
    @EnvironmentObject private var accountStore: AccountStore
    @EnvironmentObject private var projectStore: ProjectStore
    @EnvironmentObject private var engine: EngineManager
    @EnvironmentObject private var statusMonitor: StatusMonitor

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

            if accountStore.isEffectivelySignedIn {
                signedIn
            } else {
                signedOut
            }
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

            HStack(spacing: 10) {
                Menu {
                    ForEach(Environment.all) { environment in
                        Button {
                            signIn(environment)
                        } label: {
                            Text("Sign in to \(environment.shortLabel)")
                        }
                    }
                } label: {
                    Label("Add account", systemImage: "plus")
                        .font(.callout)
                }
                .menuStyle(.borderlessButton)
                .fixedSize()
                .disabled(accountStore.isSigningIn)

                Spacer(minLength: 0)

                Button("Open Memory") {
                    WindowRouter.shared.requestMainWindow()
                    NSApp.activate(ignoringOtherApps: true)
                }
                .buttonStyle(.link)
            }

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
            Text("Choose an environment to sign in.")
                .font(.caption)
                .foregroundStyle(.secondary)

            Button {
                signIn(.prod)
            } label: {
                Text("Sign in to Prod")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)

            Button {
                signIn(.dev)
            } label: {
                Text("Sign in to Dev")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.bordered)

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

    // MARK: - Actions

    private func signIn(_ environment: Environment) {
        Task {
            guard (try? await accountStore.signIn(environment: environment)) != nil else { return }
            let token = (try? await accountStore.currentAccessToken()) ?? ""
            await projectStore.loadProjects(accessToken: token)
        }
    }

    private func switchAccount(_ id: String) {
        Task { try? await accountStore.switchTo(accountID: id) }
    }
}
