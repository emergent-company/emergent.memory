import SwiftUI

/// Detail sheet for one hosted MCP server: edit its config, toggle it on/off,
/// inspect the tools discovered on it, and delete it (with confirmation).
///
/// The enabled toggle applies immediately; other config changes are applied on
/// Save (a full `PUT .../config` replace).
struct MCPServerDetailView: View {
    let server: HostedMCPServer
    @ObservedObject var store: MCPServersStore
    // Qualified: the app declares its own `Environment` type (Sources/Environment.swift),
    // which shadows SwiftUI's, so the property wrapper must be namespaced.
    @SwiftUI.Environment(\.dismiss) private var dismiss

    @State private var form: MCPServerForm?
    @State private var status: HostedMCPServerStatus?
    @State private var tools: [HostedMCPTool] = []
    @State private var isLoading = true
    @State private var isSaving = false
    @State private var errorMessage: String?
    @State private var saveConfirmation: String?
    @State private var confirmDelete = false

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()

            if form == nil && isLoading {
                ConnectorLoadingRow(caption: "Loading server…")
                    .padding()
                Spacer()
            } else if form == nil {
                unavailableState
            } else {
                content
            }

            Divider()
            footer
        }
        .frame(width: 560, height: 640)
        .task { await reload() }
        .confirmationDialog("Delete \(server.name)?",
                            isPresented: $confirmDelete,
                            titleVisibility: .visible) {
            Button("Delete Server", role: .destructive) { deleteServer() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This removes the server from the connector's local config and stops hosting its tools. It cannot be undone.")
        }
    }

    // MARK: - Header

    private var header: some View {
        HStack(alignment: .center, spacing: 12) {
            MCPStatusDot(color: MCPStatusStyle.color(enabled: form?.enabled ?? server.enabled,
                                                     connected: status?.connected ?? false,
                                                     error: status?.error))
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 8) {
                    Text(server.name.isEmpty ? "Untitled server" : server.name)
                        .font(.title3.weight(.semibold))
                        .lineLimit(1)
                    MCPTransportBadge(transport: form?.transport ?? server.transport)
                }
                Text(statusText)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
            Toggle("Enabled", isOn: enabledBinding)
                .labelsHidden()
                .toggleStyle(.switch)
                .disabled(form == nil)
                .help((form?.enabled ?? server.enabled) ? "Disable this server" : "Enable this server")
        }
        .padding(16)
    }

    private var statusText: String {
        guard let form else { return "Loading…" }
        if !form.enabled { return "Disabled" }
        if let error = status?.error, !error.isEmpty { return "Error: \(error)" }
        if status?.connected == true { return "Connected" }
        return "Not connected"
    }

    private var enabledBinding: Binding<Bool> {
        Binding(
            get: { form?.enabled ?? server.enabled },
            set: { newValue in Task { await applyEnabled(newValue) } }
        )
    }

    // MARK: - Content

    private var content: some View {
        Form {
            MCPServerFormFields(form: Binding(
                get: { self.form ?? MCPServerForm() },
                set: { self.form = $0 }
            ), showsEnabled: false)

            if let errorMessage {
                Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                    .font(.caption)
                    .foregroundStyle(.orange)
                    .fixedSize(horizontal: false, vertical: true)
            }

            toolsSection
        }
        .formStyle(.grouped)
    }

    private var toolsSection: some View {
        Section {
            if let error = status?.error, !error.isEmpty {
                Label(error, systemImage: "exclamationmark.triangle")
                    .font(.caption)
                    .foregroundStyle(.red)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if tools.isEmpty {
                Text(status?.connected == true ? "No tools reported." : "No tools discovered.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            } else {
                ForEach(tools) { tool in
                    MCPToolShareRow(
                        tool: tool,
                        displayName: displayName(for: tool),
                        isShared: isShared(tool),
                        isSaving: store.isSavingTool(server: server.name, tool: tool.name),
                        isBusy: store.isSavingTools(server: server.name),
                        onToggle: { newValue in
                            Task { await setShared(tool, newValue) }
                        }
                    )
                }
            }
        } header: {
            HStack {
                Text("Discovered Tools")
                Spacer()
                if store.isSavingTools(server: server.name) {
                    ProgressView()
                        .controlSize(.small)
                        .help("Saving sharing changes…")
                }
                if !tools.isEmpty {
                    Text("\(sharedCount) of \(tools.count) shared")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        } footer: {
            Text("Choose which of this server's tools are shared with Memory. Unshared tools stay on this Mac and are never sent to Memory.")
        }
    }

    /// The server-local names currently listed in `disabled_tools`.
    private var disabledToolNames: Set<String> {
        Set(form?.disabledTools ?? [])
    }

    /// The name to show for a tool: the server's own, un-namespaced name, which
    /// is what `disabled_tools` stores and what the toggle adds or removes.
    private func displayName(for tool: HostedMCPTool) -> String {
        MCPServersStore.ownToolName(tool.name, server: server.name)
    }

    private func isShared(_ tool: HostedMCPTool) -> Bool {
        !disabledToolNames.contains(displayName(for: tool))
    }

    private var sharedCount: Int {
        tools.reduce(0) { $0 + (isShared($1) ? 1 : 0) }
    }

    /// Appends a row for every configured `disabled_tools` entry the live tools
    /// endpoint no longer reports (disabled tools are filtered out of the
    /// discovered list), so a tool switched off from here can always be switched
    /// back on.
    private func withConfiguredDisabledTools(_ discovered: [HostedMCPTool],
                                             disabled: Set<String>) -> [HostedMCPTool] {
        var result = discovered
        let present = Set(result.map { displayName(for: $0) })
        for own in disabled.sorted() where !present.contains(own) {
            result.append(HostedMCPTool(name: "\(server.name)_\(own)",
                                        description: "",
                                        inputSchema: nil))
        }
        return result
    }

    private var unavailableState: some View {
        VStack(alignment: .leading, spacing: 10) {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.title3)
                .foregroundStyle(.orange)
            Text("Couldn't load this server")
                .font(.headline)
            Text(errorMessage ?? "The connector's management API did not return this server.")
                .font(.callout)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            Button("Retry") { Task { await reload() } }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(24)
    }

    // MARK: - Footer

    private var footer: some View {
        HStack(spacing: 12) {
            Button("Delete", role: .destructive) { confirmDelete = true }
                .disabled(isSaving || store.isSavingTools(server: server.name))
            if let saveConfirmation {
                Text(saveConfirmation)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            Button("Close") { dismiss() }
                .keyboardShortcut(.cancelAction)
            Button {
                save()
            } label: {
                if isSaving {
                    ProgressView().controlSize(.small)
                } else {
                    Text("Save")
                }
            }
            .keyboardShortcut(.defaultAction)
            .disabled(isSaving || form == nil || store.isSavingTools(server: server.name))
        }
        .padding(16)
    }

    // MARK: - Loading / actions

    private func reload() async {
        isLoading = true
        defer { isLoading = false }
        guard let detail = await store.detail(name: server.name) else {
            errorMessage = store.errorMessage
            return
        }
        errorMessage = nil
        // Only seed the form on first load so a retry preserves unsaved edits.
        if form == nil { form = MCPServerForm(config: detail.config) }
        status = detail.status
        let fetched = await store.fetchTools(name: server.name)
        tools = withConfiguredDisabledTools(fetched.isEmpty ? detail.status.tools : fetched,
                                            disabled: Set(detail.config.disabledTools ?? []))
    }

    /// Applies a sharing toggle: updates the form optimistically so the switch
    /// moves immediately, then persists through the store. On failure the
    /// previous list is restored and the error shown.
    private func setShared(_ tool: HostedMCPTool, _ shared: Bool) async {
        let own = displayName(for: tool)
        let previous = form?.disabledTools
        form?.disabledTools = optimisticDisabled(own: own, shared: shared)
        errorMessage = nil

        let succeeded = await store.setToolShared(server: server.name, tool: tool, shared: shared)
        if succeeded {
            if let refreshed = store.details[server.name]?.config.disabledTools {
                form?.disabledTools = refreshed
            }
            if let detail = store.details[server.name] {
                status = detail.status
            }
        } else {
            form?.disabledTools = previous
            errorMessage = store.errorMessage
        }
    }

    private func optimisticDisabled(own: String, shared: Bool) -> [String]? {
        var list = form?.disabledTools ?? []
        if shared {
            list.removeAll { $0 == own }
        } else if !list.contains(own) {
            list.append(own)
        }
        return list.isEmpty ? nil : list
    }

    private func applyEnabled(_ enabled: Bool) async {
        let previous = form?.enabled
        form?.enabled = enabled
        let succeeded = await store.setEnabled(name: server.name, enabled: enabled)
        if succeeded {
            if let detail = await store.detail(name: server.name) {
                status = detail.status
                if form == nil { form = MCPServerForm(config: detail.config) }
            }
        } else {
            form?.enabled = previous ?? enabled
            errorMessage = store.errorMessage
        }
    }

    private func save() {
        guard let form, !isSaving else { return }
        let existing = store.existingNames.subtracting([server.name])
        if let message = form.validationMessage(existingNames: existing) {
            errorMessage = message
            return
        }
        isSaving = true
        errorMessage = nil
        saveConfirmation = nil
        let config = form.config()
        Task {
            let succeeded = await store.update(name: server.name, config: config)
            isSaving = false
            if succeeded {
                saveConfirmation = "Saved."
                if let detail = await store.detail(name: server.name) {
                    status = detail.status
                }
            } else {
                errorMessage = store.errorMessage
            }
        }
    }

    private func deleteServer() {
        Task {
            let succeeded = await store.delete(name: server.name)
            if succeeded {
                dismiss()
            } else {
                errorMessage = store.errorMessage
            }
        }
    }
}

// MARK: - Tool sharing row

/// One row in the detail view's tool list: a tool with a per-server sharing
/// toggle. `isShared == false` means the tool's name is in the server's
/// `disabled_tools` list, so it is not registered with Memory.
private struct MCPToolShareRow: View {
    let tool: HostedMCPTool
    let displayName: String
    let isShared: Bool
    let isSaving: Bool
    /// True while any sharing write for this server is in flight; the toggle is
    /// held disabled so two read-modify-write requests cannot race.
    let isBusy: Bool
    let onToggle: (Bool) -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(displayName)
                    .font(.callout.weight(.medium))
                    .textSelection(.enabled)
                if !tool.description.isEmpty {
                    Text(tool.description)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if !isShared {
                    Text("Not shared")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
            }
            Spacer(minLength: 0)
            if isSaving {
                ProgressView()
                    .controlSize(.small)
            }
            Toggle("Shared", isOn: Binding(get: { isShared }, set: onToggle))
                .labelsHidden()
                .toggleStyle(.switch)
                .disabled(isBusy)
                .help(isShared ? "Stop sharing \(displayName) with Memory"
                               : "Share \(displayName) with Memory")
        }
        .padding(.vertical, 2)
    }
}
