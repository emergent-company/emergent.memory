import SwiftUI

// MARK: - Accounts

/// Lists EVERY signed-in account (identity + environment badge + active
/// checkmark) with per-account sign-out and an "Add account" action.
struct AccountsCard: View {
    let accounts: [Account]
    let activeAccountID: String?
    let isSigningIn: Bool
    let needsReauthentication: (String) -> Bool
    let onSignOut: (Account) -> Void
    let onAddAccount: () -> Void
    let onSignInAgain: (Account) -> Void

    var body: some View {
        ConnectorCard(title: "Accounts", systemImage: "person.2.circle") {
            VStack(alignment: .leading, spacing: 14) {
                ForEach(accounts) { account in
                    accountRow(account)
                }
                Divider()
                addAccountControl
                Text("Signing an account out stops the connector if it is active and clears that account's saved session. Other accounts stay signed in.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private func accountRow(_ account: Account) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                AccountRowLabel(account: account,
                                showsAvatar: true,
                                avatarSize: 40,
                                isActive: account.id == activeAccountID)
                if needsReauthentication(account.id) {
                    Label("Needs sign-in again", systemImage: "exclamationmark.triangle")
                        .font(.caption)
                        .foregroundStyle(.orange)
                }
            }
            if needsReauthentication(account.id) {
                Button {
                    onSignInAgain(account)
                } label: {
                    Text("Sign in again")
                }
                .buttonStyle(.borderedProminent)
                .accessibilityLabel(Text("Sign in again \(account.displayTitle)"))
                .controlSize(.small)
                .disabled(isSigningIn)
            }
            Button(role: .destructive) {
                onSignOut(account)
            } label: {
                Text("Sign out")
            }
            .accessibilityLabel(Text("Sign out \(account.displayTitle)"))
            .controlSize(.small)
            .disabled(isSigningIn)
        }
    }

    /// Single Production sign-in action for adding an account — this page
    /// offers no environment choice.
    private var addAccountControl: some View {
        Button(action: onAddAccount) {
            Label("Add account", systemImage: "person.badge.plus")
        }
        .disabled(isSigningIn)
    }
}

// MARK: - Sign-in card

/// The single sign-in prompt card, shown only while signed out.
struct SignInCard: View {
    let onSignIn: () -> Void

    var body: some View {
        ConnectorCard(title: "Accounts", systemImage: "person.2.circle") {
            VStack(alignment: .leading, spacing: 10) {
                Text("Sign in with Memory to connect your account and projects.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)

                Button(action: onSignIn) {
                    Label("Sign in with Memory", systemImage: "person.crop.circle.badge.checkmark")
                }
                .buttonStyle(.borderedProminent)
            }
        }
    }
}

// MARK: - Project

/// The "Project" card: active-project identity (name/id, organisation) plus the
/// loading/error/signed-out states driven by `ProjectStore`.
struct ProjectCard: View {
    let projects: [ProjectInfo]
    let state: ProjectStore.State
    let activeProject: ProjectInfo?
    let activeProjectName: String?
    let activeProjectID: String?
    let snapshot: IdentitySnapshot?
    let isSigningIn: Bool
    let onTryAgain: () -> Void
    let onSignIn: () -> Void

    var body: some View {
        ConnectorCard(title: "Project", systemImage: "building.2") {
            signedInProjectSection
        }
    }

    @ViewBuilder
    private var signedInProjectSection: some View {
        if projects.isEmpty {
            switch state {
            case .idle, .loading:
                ConnectorLoadingRow(caption: "Loading projects…")
            case .signedOut:
                signedOutState
            case let .error(message):
                projectErrorState(message)
            case .loaded:
                emptyProjectsState
            }
        } else {
            VStack(alignment: .leading, spacing: 14) {
                activeProjectDetails
                switchControl

                if case .signedOut = state {
                    signedOutState
                } else if case let .error(message) = state {
                    inlineError(message)
                }
            }
        }
    }

    private var switchControl: some View {
        // The page hosts the same list the toolbar panel shows, inline instead
        // of in a popover: here the list is page content, not a menu. The
        // panel's own header is off because the active project is rendered
        // above it, and "Manage projects…" is pointless on the page that does
        // exactly that.
        VStack(alignment: .leading, spacing: 2) {
            SwitcherSectionHeader("Projects")
            ProjectSwitcherPanel(showsManageAction: false, showsHeader: false)
        }
    }

    private var activeProjectDetails: some View {
        let orgName = snapshot?.organizationName
        let orgID = snapshot?.organizationID ?? activeProject?.orgID
        return VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 8) {
                Text(activeProjectName ?? activeProject?.name ?? "No project selected")
                    .font(.title3.weight(.semibold))
                    .lineLimit(1)
                    .truncationMode(.tail)
                IdentifierReveal(identifier: activeProjectID ?? activeProject?.id ?? "")
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
            Button("Try Again") { onTryAgain() }
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
                Button(action: onSignIn) {
                    if isSigningIn {
                        HStack(spacing: 6) {
                            ProgressView().controlSize(.small)
                            Text("Signing in…")
                        }
                    } else {
                        Label("Sign in", systemImage: "person.crop.circle.badge.checkmark")
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(isSigningIn)

                Button("Try again") { onTryAgain() }
                    .disabled(isSigningIn)
            }
        }
    }

    private func inlineError(_ message: String) -> some View {
        Label(message, systemImage: "exclamationmark.triangle")
            .font(.caption)
            .foregroundStyle(.orange)
            .fixedSize(horizontal: false, vertical: true)
    }
}

// MARK: - Error banner

struct ErrorBanner: View {
    let message: String

    var body: some View {
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
