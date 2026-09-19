import AppKit
import SwiftUI

/// About page: app/engine versions, instance identity, and attribution.
///
/// This is also the app's only surface that offers Development sign-in — a
/// non-prominent developer affordance below the cards, deliberately unlike the
/// primary Production sign-in shown everywhere else.
struct AboutPage: View {
    @EnvironmentObject private var settings: ConnectorSettings
    @EnvironmentObject private var accountStore: AccountStore
    @EnvironmentObject private var projectStore: ProjectStore
    @EnvironmentObject private var updater: UpdaterModel
    @ObservedObject private var statusMonitor = StatusMonitor.shared

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                header
                ConnectorCard(title: "Details", systemImage: "info.circle") {
                    VStack(alignment: .leading, spacing: 10) {
                        ConnectorInfoRow(label: "App version", value: appVersion)
                        ConnectorInfoRow(label: "Build", value: buildNumber, monospaced: true)
                        ConnectorInfoRow(label: "Engine version", value: engineVersion, monospaced: true)
                        IdentifierInfoRow(label: "Instance", identifier: instanceID)
                        if !settings.serverURL.isEmpty {
                            ConnectorInfoRow(label: "Server", value: settings.serverURL)
                        }
                    }
                }
                ConnectorCard(title: "Updates", systemImage: "arrow.triangle.2.circlepath") {
                    VStack(alignment: .leading, spacing: 12) {
                        ConnectorInfoRow(label: "Current version", value: "\(appVersion) (\(buildNumber))")

                        HStack(alignment: .firstTextBaseline, spacing: 12) {
                            Text("Status")
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                                .frame(width: 130, alignment: .leading)
                            HStack(spacing: 6) {
                                updateStatusIndicator
                                Text(updater.statusText)
                                    .font(.subheadline)
                                    .foregroundStyle(.secondary)
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                            Spacer(minLength: 0)
                        }

                        Toggle("Automatically check for updates", isOn: $updater.automaticallyChecksForUpdates)
                            .font(.subheadline)
                            .disabled(!updater.isUpdaterAvailable)

                        HStack {
                            Button("Check for Updates…") {
                                updater.checkForUpdates()
                            }
                            .disabled(!updater.canCheckForUpdates)
                            Spacer(minLength: 0)
                        }
                    }
                }
                ConnectorCard(title: "About", systemImage: "heart") {
                    VStack(alignment: .leading, spacing: 12) {
                        Text("Memory")
                            .font(.headline)
                        Text("A native menu-bar companion that runs the Memory connector engine and connects your local Apple tools to Memory.")
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                        Text("Sidebar structure and card styling derived from Diane (Emergent Company, MIT licensed).")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                            .fixedSize(horizontal: false, vertical: true)

                        if let url = mcpNodesURL {
                            Link(destination: url) {
                                Label("Open MCP nodes in Memory", systemImage: "arrow.up.right.square")
                            }
                            .font(.callout)
                        }
                    }
                }
                developmentSignIn
            }
            .padding(24)
            .frame(maxWidth: 720, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
        .navigationTitle("About")
    }

    // MARK: - Header

    private var header: some View {
        HStack(alignment: .center, spacing: 16) {
            Image(nsImage: NSApp.applicationIconImage)
                .resizable()
                .frame(width: 64, height: 64)
            VStack(alignment: .leading, spacing: 3) {
                Text("Memory")
                    .font(.title2.weight(.semibold))
                Text("Version \(appVersion) (\(buildNumber))")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
        }
    }

    // MARK: - Updates

    /// Leading glyph for the update status line. A running check shows a
    /// spinner; a failed check is the only one to use the warning colour, which
    /// matches the Development sign-in error styling on this page.
    @ViewBuilder
    private var updateStatusIndicator: some View {
        switch updater.state {
        case .checking:
            ProgressView()
                .controlSize(.small)
        case .upToDate:
            Image(systemName: "checkmark.circle")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        case .updateAvailable:
            Image(systemName: "arrow.down.circle")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        case .installing:
            Image(systemName: "arrow.triangle.2.circlepath")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        case .failed:
            Image(systemName: "exclamationmark.triangle")
                .font(.subheadline)
                .foregroundStyle(.orange)
        case .idle, .unavailable:
            Image(systemName: "circle.dashed")
                .font(.subheadline)
                .foregroundStyle(.tertiary)
        }
    }

    // MARK: - Development sign-in

    /// The Development environment this page offers, derived from the shared
    /// per-surface policy rather than named directly.
    private var developmentEnvironment: Environment? {
        Environment.signInEnvironments(for: .about).first { $0 != Environment.primary }
    }

    /// Developer escape hatch: sign in to the internal Development environment.
    ///
    /// Kept subordinate on purpose — no card, caption-sized secondary/link
    /// styling, and placed last on the page — so it never reads as a primary
    /// action next to the Production sign-in used by the rest of the app.
    @ViewBuilder
    private var developmentSignIn: some View {
        if let environment = developmentEnvironment {
            VStack(alignment: .leading, spacing: 8) {
                Text("Development")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)

                Text("Development sign-in targets \(environment.name) at \(environment.serverURLString).")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)

                Button {
                    signInToDevelopment(environment)
                } label: {
                    if accountStore.isSigningIn {
                        HStack(spacing: 6) {
                            ProgressView().controlSize(.small)
                            Text("Signing in…")
                        }
                    } else {
                        Text("Sign in to Development")
                    }
                }
                .buttonStyle(.link)
                .font(.caption)
                .disabled(accountStore.isSigningIn)

                if let message = accountStore.lastError {
                    Label(message, systemImage: "exclamationmark.triangle")
                        .font(.caption)
                        .foregroundStyle(.orange)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .padding(.horizontal, 4)
        }
    }

    /// Signs in to the Development environment and reloads projects, matching
    /// the other sign-in surfaces. `AccountStore.signIn` records any failure in
    /// `lastError`, surfaced inline above; a cancelled or failed Dev sign-in
    /// must not disturb the page.
    private func signInToDevelopment(_ environment: Environment) {
        Task {
            guard (try? await accountStore.signIn(environment: environment)) != nil else { return }
            let token = (try? await accountStore.currentAccessToken()) ?? ""
            await projectStore.loadProjects(accessToken: token)
        }
    }

    // MARK: - Values

    private var appVersion: String {
        Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "—"
    }

    private var buildNumber: String {
        Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? "—"
    }

    private var engineVersion: String {
        guard let version = statusMonitor.snapshot?.version, !version.isEmpty else { return "—" }
        return version
    }

    private var instanceID: String {
        if let id = statusMonitor.snapshot?.instanceID, !id.isEmpty { return id }
        return settings.instanceID
    }

    private var mcpNodesURL: URL? {
        let base = settings.serverURL.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !base.isEmpty else { return nil }
        let trimmed = base.hasSuffix("/") ? String(base.dropLast()) : base
        return URL(string: trimmed + "/settings/mcp-nodes")
    }
}
