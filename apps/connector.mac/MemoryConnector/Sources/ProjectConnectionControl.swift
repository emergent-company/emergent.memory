import SwiftUI

/// Reusable per-project connection control.
///
/// A single switch that connects the engine relay to the active project or
/// disconnects it (stopping the engine). Exactly one project can be connected
/// at a time: when another project is connected an inline note explains that
/// connecting here moves the connection.
///
/// Editing tools is independent of connection state — this control never
/// touches tool enablement. Used on the Dashboard and on Project & Account.
struct ProjectConnectionControl: View {
    @EnvironmentObject private var accountStore: AccountStore
    @EnvironmentObject private var projectStore: ProjectStore
    @EnvironmentObject private var appState: AppState

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
                    .disabled(busy)
            }

            if let other = otherConnectedName(projectID: projectID) {
                Label("Connected to \(other) — connecting here will move the connection.",
                      systemImage: "info.circle")
                    .font(.caption)
                    .foregroundStyle(.orange)
                    .fixedSize(horizontal: false, vertical: true)
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

    private func connectionSubtitle(projectID: String) -> String {
        if projectStore.isConnected(projectID) {
            return "The connector is serving this project's local tools."
        }
        return "The connector is stopped until a project is connected."
    }

    /// Name of the currently connected project when it is NOT `projectID`.
    private func otherConnectedName(projectID: String) -> String? {
        guard let connected = projectStore.connectedProjectID, connected != projectID else {
            return nil
        }
        return projectStore.projects.first { $0.id == connected }?.name ?? "another project"
    }

    private func binding(projectID: String) -> Binding<Bool> {
        Binding(
            get: { projectStore.isConnected(projectID) },
            set: { on in
                guard !busy else { return }
                if on { connect(projectID) } else { disconnect() }
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
                case .error(let message):
                    error = message
                case .signedOut:
                    error = "Your session expired. Sign in to connect a project."
                default:
                    break
                }
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    private func disconnect() {
        error = nil
        projectStore.disconnect()
    }
}
