import AppKit
import SwiftUI

/// About page: app/engine versions, instance identity, and attribution.
struct AboutPage: View {
    @EnvironmentObject private var settings: ConnectorSettings
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
