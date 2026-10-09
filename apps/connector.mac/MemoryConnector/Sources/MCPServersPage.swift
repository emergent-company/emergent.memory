import SwiftUI

/// Hosted MCP servers page: the servers the connector hosts on this Mac, each
/// with its transport, enabled state, live connection status, and discovered
/// tool count. Selecting a row opens ``MCPServerDetailView``; the toggle
/// enables/disables a server immediately.
///
/// Server commands, URLs, environment variables, and headers stay in the
/// connector's local config — only the discovered tools are shared with Memory.
struct MCPServersPage: View {
    @SwiftUI.Environment(EngineManager.self) private var engine
    @SwiftUI.Environment(ProjectStore.self) private var projectStore
    @SwiftUI.Environment(AppState.self) private var appState

    @State private var store = MCPServersStore()
    @State private var showingAdd = false
    @State private var showingRegistry = false
    @State private var selectedServer: HostedMCPServer?

    /// Whether the connector's management API is reachable for this page.
    private var availability: MCPServersAvailability {
        MCPServersAvailability.resolve(engineState: engine.state,
                                       hasConnectedProject: projectStore.hasConnectedProject)
    }

    var body: some View {
        Form {
            Section {
                listContent
            } header: {
                HStack {
                    Text("Hosted MCP Servers")
                    Spacer()
                    Button {
                        showingRegistry = true
                    } label: {
                        Label("Browse registry", systemImage: "magnifyingglass")
                    }
                    .buttonStyle(.borderless)
                    .disabled(!availability.isAvailable)
                    .help("Search the MCP registry and add a suggested server")
                    Button {
                        showingAdd = true
                    } label: {
                        Label("Add", systemImage: "plus")
                    }
                    .buttonStyle(.borderless)
                    .disabled(!availability.isAvailable)
                    .help("Add a hosted MCP server")
                }
            } footer: {
                Text("MCP servers are hosted by the connector on this Mac. Commands, URLs, environment variables, and headers stay in the connector's local config and are never sent to Memory.")
            }

            if availability.isAvailable, let status = store.status {
                relaySection(status)
            }
        }
        .formStyle(.grouped)
        .navigationTitle("Hosted MCP Servers")
        .task {
            store.startPolling()
        }
        // Reload whenever the connector becomes available (and never while it is
        // starting, stopped, or failed). `availability` is Equatable so the task
        // re-runs on each transition.
        .task(id: availability) {
            guard availability.isAvailable else { return }
            await store.load()
        }
        .onDisappear { store.stopPolling() }
        .sheet(isPresented: $showingAdd) {
            AddMCPServerSheet(store: store)
        }
        .sheet(isPresented: $showingRegistry) {
            MCPRegistryBrowseSheet(store: store)
        }
        .sheet(item: $selectedServer) { server in
            MCPServerDetailView(server: server, store: store)
        }
    }

    // MARK: - List

    @ViewBuilder
    private var listContent: some View {
        switch availability {
        case .noConnectedProject:
            notConnectedRow
        case .connecting:
            ConnectorLoadingRow(caption: "Connecting…")
        case .unavailable:
            unavailableRow
        case .available:
            availableContent
        }
    }

    @ViewBuilder
    private var availableContent: some View {
        if store.servers.isEmpty {
            if store.phase == .loading || store.phase == .idle {
                ConnectorLoadingRow(caption: "Loading hosted MCP servers…")
            } else if let error = store.errorMessage {
                errorRow(error)
            } else {
                emptyRow
            }
        } else {
            ForEach(store.servers) { server in
                serverRow(server)
            }
        }
    }

    private var emptyRow: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: "server.rack")
                .font(.title3)
                .foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 4) {
                Text("No hosted MCP servers")
                    .font(.headline)
                Text("Add a local MCP server to share its tools through Memory.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
            Button("Browse registry…") { showingRegistry = true }
            Button("Add Server…") { showingAdd = true }
        }
        .padding(.vertical, 4)
    }

    private func errorRow(_ message: String) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.title3)
                .foregroundStyle(.orange)
            VStack(alignment: .leading, spacing: 4) {
                Text("Can't load hosted MCP servers")
                    .font(.headline)
                Text(message)
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
            Button("Retry") { Task { await store.load() } }
        }
        .padding(.vertical, 4)
    }

    private func serverRow(_ server: HostedMCPServer) -> some View {
        HStack(alignment: .center, spacing: 12) {
            Button {
                selectedServer = server
            } label: {
                serverRowLabel(server)
            }
            .buttonStyle(.plain)
            .help(server.managed ? "View \(server.name)" : "Edit \(server.name)")

            // Managed servers are toggled in Memory, not here.
            if !server.managed {
                Toggle("", isOn: enabledBinding(server))
                    .labelsHidden()
                    .toggleStyle(.switch)
                    .accessibilityLabel(Text(AccessibleNames.serverEnabled(server.name)))
                    .help(server.enabled ? "Disable \(server.name)" : "Enable \(server.name)")
            }
        }
        .padding(.vertical, 4)
    }

    private func serverRowLabel(_ server: HostedMCPServer) -> some View {
        HStack(alignment: .center, spacing: 12) {
            MCPStatusDot(color: MCPStatusStyle.color(enabled: server.enabled,
                                                     connected: server.connected,
                                                     error: server.error))
            VStack(alignment: .leading, spacing: 3) {
                serverTitleRow(server)
                serverDetailRow(server)
                if let error = server.error, !error.isEmpty {
                    Text(error)
                        .font(.caption)
                        .foregroundStyle(.red)
                        .lineLimit(2)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            Spacer(minLength: 8)
            Image(systemName: "chevron.right")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.tertiary)
        }
        .contentShape(Rectangle())
    }

    private func serverTitleRow(_ server: HostedMCPServer) -> some View {
        HStack(spacing: 8) {
            Text(server.name.isEmpty ? "Untitled server" : server.name)
                .font(.callout.weight(.medium))
                .lineLimit(1)
            MCPTransportBadge(transport: server.transport)
            if server.managed {
                MCPManagedBadge()
            }
        }
    }

    private func serverDetailRow(_ server: HostedMCPServer) -> some View {
        HStack(spacing: 6) {
            Text(MCPStatusStyle.label(enabled: server.enabled,
                                      connected: server.connected,
                                      error: server.error))
                .font(.caption)
                .foregroundStyle(.secondary)
            if server.toolCount > 0 {
                Text("·")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
                Text("\(server.toolCount) \(server.toolCount == 1 ? "tool" : "tools")")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private func enabledBinding(_ server: HostedMCPServer) -> Binding<Bool> {
        Binding(
            get: { store.servers.first { $0.name == server.name }?.enabled ?? server.enabled },
            set: { newValue in
                Task { await store.setEnabled(name: server.name, enabled: newValue) }
            }
        )
    }

    // MARK: - Relay

    private func relaySection(_ status: RelayRuntimeStatus) -> some View {
        Section {
            HStack(spacing: 8) {
                MCPStatusDot(color: status.connected ? .green : .orange)
                Text(status.connected ? "Connected to Memory" : "Not connected to Memory")
                    .font(.callout)
                Spacer(minLength: 0)
                Text("\(status.toolCount) \(status.toolCount == 1 ? "tool" : "tools") registered")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        } header: {
            Text("Relay")
        }
    }
}

// MARK: - Availability-gated states

extension MCPServersPage {
    /// No project connected: the connector is intentionally stopped, so this is
    /// a setup prompt rather than an error.
    private var notConnectedRow: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: "person.crop.circle.badge.questionmark")
                .font(.title3)
                .foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 4) {
                Text("Connect a project to manage hosted MCP servers")
                    .font(.headline)
                Text("Hosted MCP servers run through the connector while a Memory project is connected.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
            Button("Open Project & Account") { appState.selectedSidebarItem = .project }
        }
        .padding(.vertical, 4)
    }

    /// A project is connected but the connector is not reachable. Kept generic:
    /// no engine or loopback-API detail is shown.
    private var unavailableRow: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.title3)
                .foregroundStyle(.orange)
            VStack(alignment: .leading, spacing: 4) {
                Text("Can't load hosted MCP servers")
                    .font(.headline)
                Text("The connector isn't available right now. Try again in a moment.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
            Button("Retry") { Task { await store.load() } }
        }
        .padding(.vertical, 4)
    }
}

// MARK: - Shared status affordances

/// Colored status dot, matching the footer/menu-bar palette: green connected,
/// orange connecting/starting, red error, gray disabled.
enum MCPStatusStyle {
    static func color(enabled: Bool, connected: Bool, error: String?) -> Color {
        if !enabled {
            return .secondary
        }
        if connected {
            return .green
        }
        if let error, !error.isEmpty {
            return .red
        }
        return .orange
    }

    static func label(enabled: Bool, connected: Bool, error: String?) -> LocalizedStringResource {
        if !enabled {
            return "Disabled"
        }
        if connected {
            return "Connected"
        }
        if let error, !error.isEmpty {
            return "Error"
        }
        return "Not connected"
    }

    /// Status line for the detail sheet: `label`, but with the server's error
    /// text inlined and a loading placeholder until the detail arrives.
    static func detailLabel(enabled: Bool, connected: Bool, error: String?,
                            loading: Bool = false) -> LocalizedStringResource
    {
        if loading {
            return "Loading…"
        }
        if !enabled {
            return "Disabled"
        }
        if let error, !error.isEmpty {
            return "Error: \(error)"
        }
        return connected ? "Connected" : "Not connected"
    }
}

struct MCPStatusDot: View {
    let color: Color

    var body: some View {
        Circle()
            .fill(color)
            .frame(width: 8, height: 8)
    }
}

/// Small capsule showing a server's transport (`stdio` / `http` / `sse`).
struct MCPTransportBadge: View {
    let transport: MCPTransport

    var body: some View {
        Text(transport.displayName)
            .font(.caption2.weight(.semibold))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(
                Capsule().fill(.quaternary.opacity(0.6))
            )
    }
}

/// Small capsule marking a server as configured in Memory. Such servers are
/// read-only locally and are edited in Memory, not in this app.
struct MCPManagedBadge: View {
    var body: some View {
        Text("Managed")
            .font(.caption2.weight(.semibold))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(
                Capsule().fill(.quaternary.opacity(0.6))
            )
            .help("Managed in Memory. Edit this server in Memory, not here.")
    }
}
