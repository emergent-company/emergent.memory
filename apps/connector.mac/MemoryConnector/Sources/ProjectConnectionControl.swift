import SwiftUI

/// Reusable per-project connection control.
///
/// A switch that connects an engine child to the active project or disconnects
/// it (stopping that project's child). Any number of projects may be connected
/// at once — connecting here never disconnects another project.
///
/// Editing tools is independent of connection state — this control never
/// touches tool enablement. Used on the Dashboard and on Project & Account.
struct ProjectConnectionControl: View {
    @SwiftUI.Environment(AccountStore.self) private var accountStore
    @SwiftUI.Environment(ProjectStore.self) private var projectStore
    @SwiftUI.Environment(RecentProjectsStore.self) private var recentProjects
    @SwiftUI.Environment(AppState.self) private var appState

    /// In-flight connect/disconnect (token mint + engine restart).
    @State private var busy = false
    /// Inline failure (sign-in required, token, engine config write).
    @State private var error: String?

    var body: some View {
        ConnectorCard(title: "Connection", systemImage: "bolt.horizontal.circle") {
            content
        }
    }

    // MARK: - States

    @ViewBuilder
    private var content: some View {
        if !accountStore.isEffectivelySignedIn {
            signedOutState
        } else if let projectID = projectStore.activeProjectID, !projectID.isEmpty {
            connectedState(projectID: projectID)
        } else {
            Text("Select a project to connect.")
                .font(.callout)
                .foregroundStyle(.secondary)
        }
    }

    private var signedOutState: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: "person.crop.circle.badge.questionmark")
                .foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 2) {
                Text("Sign in to connect a project")
                    .font(.callout)
                Text("The connector runs only for the connected project.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
            Button {
                appState.selectedSidebarItem = .connection
            } label: {
                Label("Sign in", systemImage: "person.crop.circle.badge.checkmark")
            }
            .buttonStyle(.link)
            .font(.caption)
        }
    }

    private func connectedState(projectID: String) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .center, spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Connected to this project")
                        .font(.callout.weight(.medium))
                    Text(connectionSubtitle(projectID: projectID))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }

                Spacer(minLength: 0)

                if busy {
                    ProgressView().controlSize(.small)
                }

                Toggle("", isOn: binding(projectID: projectID))
                    .labelsHidden()
                    .toggleStyle(.switch)
                    .accessibilityLabel(Text(AccessibleNames.projectConnection(
                        projectStore.activeProjectName
                    )))
                    .disabled(busy)
            }

            if let error {
                Label(error, systemImage: "exclamationmark.triangle")
                    .font(.caption)
                    .foregroundStyle(.orange)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    // MARK: - Derived

    private func connectionSubtitle(projectID: String) -> LocalizedStringResource {
        if projectStore.isConnected(projectID) {
            return "The connector is serving this project's local tools."
        }
        return "The connector is stopped for this project."
    }

    private func binding(projectID: String) -> Binding<Bool> {
        Binding(
            get: { projectStore.isConnected(projectID) },
            set: { isOn in
                guard !busy else { return }
                if isOn {
                    connect(projectID)
                } else {
                    disconnect(projectID)
                }
            }
        )
    }

    // MARK: - Actions

    private func connect(_ projectID: String) {
        busy = true
        error = nil
        Task {
            defer { busy = false }
            guard accountStore.isEffectivelySignedIn else {
                error = "Sign in to connect a project."
                return
            }
            do {
                let token = try await accountStore.currentAccessToken()
                await projectStore.connect(projectID: projectID, accessToken: token)
                switch projectStore.state {
                case let .error(message):
                    error = message
                case .signedOut:
                    error = "Your session expired. Sign in to connect a project."
                default:
                    // Connecting makes the project active, so it belongs in the
                    // switcher's Recent list just like an explicit switch.
                    recentProjects.record(projectID)
                }
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    private func disconnect(_ projectID: String) {
        error = nil
        projectStore.disconnect(projectID: projectID)
    }
}
