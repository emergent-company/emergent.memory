import SwiftUI

/// Connection settings page.
///
/// Auth-only: a "Memory Account" section signs in/out through the bundled
/// `memory-connector` CLI. When no account is signed in, the page shows the
/// sign-in affordance; signed in, it shows the active account. There is no
/// manual project-token entry anymore.
struct ConnectionPage: View {
    @EnvironmentObject private var accountStore: AccountStore
    @EnvironmentObject private var projectStore: ProjectStore

    @State private var alertMessage: String?
    /// Environment chosen on the signed-out sign-in surface.
    @State private var selectedEnvironment: Environment = .prod

    var body: some View {
        Form {
            memoryAccountSection
        }
        .formStyle(.grouped)
        .navigationTitle("Connection")
        .alert("Memory", isPresented: Binding(
            get: { alertMessage != nil },
            set: { if !$0 { alertMessage = nil } }
        )) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(alertMessage ?? "")
        }
    }

    // MARK: - Memory Account

    private var memoryAccountSection: some View {
        Section {
            if accountStore.isEffectivelySignedIn {
                signedInRow
            } else {
                signInControl
            }
        } header: {
            Text("Memory Account")
        } footer: {
            Text("Sign in with your Memory identity to list and switch projects. Requires a Zitadel native app using PKCE with the com.emergent.memory.connector callback scheme.")
        }
    }

    private var signedInRow: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .center, spacing: 12) {
                AccountAvatar(initials: activeInitials, size: 44)
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 8) {
                        Text(activeName)
                            .font(.title3.weight(.semibold))
                            .lineLimit(1)
                        if let environment = accountStore.activeEnvironment {
                            EnvironmentBadge(environment: environment)
                        }
                    }
                    if !activeEmail.isEmpty {
                        Text(activeEmail)
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                }
                Spacer(minLength: 0)
            }
            HStack {
                Spacer()
                Button("Sign out", role: .destructive) { signOutActive() }
            }
        }
    }

    private var signInControl: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Sign in with Memory to connect your account and projects.")
                .font(.callout)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)

            Picker("Environment", selection: $selectedEnvironment) {
                ForEach(Environment.all) { environment in
                    Text(environment.shortLabel).tag(environment)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()

            Text(selectedEnvironment.serverURLString)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .truncationMode(.middle)

            HStack {
                Button {
                    signIn(selectedEnvironment)
                } label: {
                    if accountStore.isSigningIn {
                        HStack(spacing: 6) {
                            ProgressView().controlSize(.small)
                            Text("Signing in…")
                        }
                    } else {
                        Label("Sign in with Memory", systemImage: "person.crop.circle.badge.checkmark")
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(accountStore.isSigningIn)
                Spacer()
            }

            if let message = accountStore.lastError {
                Label(message, systemImage: "exclamationmark.triangle")
                    .font(.caption)
                    .foregroundStyle(.orange)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    // MARK: - Active identity

    private var activeAccount: Account? { accountStore.activeAccount }

    private var activeName: String { activeAccount?.displayTitle ?? "" }

    private var activeEmail: String { activeAccount?.email ?? "" }

    private var activeInitials: String { activeAccount?.initials ?? "" }

    // MARK: - Actions

    private func signIn(_ environment: Environment) {
        Task {
            do {
                _ = try await accountStore.signIn(environment: environment)
                // `AppEnvironment` applies the account scope and reloads the
                // projects/identity for a NEW active account. Re-signing in to
                // the same account does not change the id, so reload here too.
                let token = (try? await accountStore.currentAccessToken()) ?? ""
                await projectStore.loadProjects(accessToken: token)
            } catch {
                alertMessage = error.localizedDescription
            }
        }
    }

    private func signOutActive() {
        guard let id = accountStore.activeAccountID else { return }
        Task { await accountStore.signOut(accountID: id) }
    }
}
