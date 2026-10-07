import SwiftUI

/// Detail sheet for one hosted MCP server: edit its config, toggle it on/off,
/// inspect the tools discovered on it, and delete it (with confirmation).
///
/// The enabled toggle applies immediately; other config changes are applied on
/// Save (a full `PUT .../config` replace). A Memory-managed server is read-only
/// here — it is edited in Memory — so its controls are hidden or disabled.
struct MCPServerDetailView: View {
    let server: HostedMCPServer
    let store: MCPServersStore
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

    /// True when Memory manages this server: either the row already knew, or
    /// the loaded list/detail does.
    private var isManaged: Bool {
        server.managed || store.isManaged(server.name)
    }

    var body: some View {
        VStack(spacing: 0) {
            MCPServerDetailHeader(
                name: server.name,
                transport: form?.transport ?? server.transport,
                enabled: form?.enabled ?? server.enabled,
                connected: status?.connected ?? false,
                error: status?.error,
                isManaged: isManaged,
                loading: form == nil,
                statusText: statusText,
                enabledBinding: enabledBinding
            )
            Divider()

            if form == nil, isLoading {
                ConnectorLoadingRow(caption: "Loading server…")
                    .padding()
                Spacer()
            } else if form == nil {
                MCPServerUnavailableView(message: errorMessage) { Task { await reload() } }
            } else {
                MCPServerDetailContent(
                    form: $form,
                    isManaged: isManaged,
                    errorMessage: errorMessage,
                    tools: tools,
                    status: status,
                    store: store,
                    serverName: server.name,
                    onToggle: { tool, shared in Task { await setShared(tool, shared) } }
                )
            }

            Divider()
            MCPServerDetailFooter(
                isManaged: isManaged,
                isSaving: isSaving,
                saveConfirmation: saveConfirmation,
                isSavingTools: store.isSavingTools(server: server.name),
                hasForm: form != nil,
                onDelete: { confirmDelete = true },
                onClose: { dismiss() },
                onSave: { save() }
            )
        }
        .frame(width: 560, height: 640)
        .task { await reload() }
        .confirmationDialog("Delete \(server.name)?",
                            isPresented: $confirmDelete,
                            titleVisibility: .visible)
        {
            Button("Delete Server", role: .destructive) { deleteServer() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This removes the server from the connector's local config and stops hosting its tools. It cannot be undone.")
        }
    }

    private var statusText: LocalizedStringResource {
        MCPStatusStyle.detailLabel(enabled: form?.enabled ?? server.enabled,
                                   connected: status?.connected ?? false,
                                   error: status?.error,
                                   loading: form == nil)
    }

    private var enabledBinding: Binding<Bool> {
        Binding(
            get: { form?.enabled ?? server.enabled },
            set: { newValue in Task { await applyEnabled(newValue) } }
        )
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
        if form == nil {
            form = MCPServerForm(config: detail.config)
        }
        status = detail.status
        let fetched = await store.fetchTools(name: server.name)
        tools = MCPServerToolSelection.displayTools(
            discovered: fetched.isEmpty ? detail.status.tools : fetched,
            disabledTools: detail.config.disabledTools,
            server: server.name
        )
    }

    /// Applies a sharing toggle: updates the form optimistically so the switch
    /// moves immediately, then persists through the store. On failure the
    /// previous list is restored and the error shown.
    private func setShared(_ tool: HostedMCPTool, _ shared: Bool) async {
        guard !isManaged else { return }
        let own = MCPServerToolSelection.displayName(for: tool, server: server.name)
        let previous = form?.disabledTools
        form?.disabledTools = MCPServerToolSelection.applying(own: own, shared: shared, to: previous)
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

    private func applyEnabled(_ enabled: Bool) async {
        guard !isManaged else { return }
        let previous = form?.enabled
        form?.enabled = enabled
        let succeeded = await store.setEnabled(name: server.name, enabled: enabled)
        if succeeded {
            if let detail = await store.detail(name: server.name) {
                status = detail.status
                if form == nil {
                    form = MCPServerForm(config: detail.config)
                }
            }
        } else {
            form?.enabled = previous ?? enabled
            errorMessage = store.errorMessage
        }
    }

    private func save() {
        guard let form, !isSaving, !isManaged else { return }
        let existing = store.reservedNames.subtracting([server.name])
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

// MARK: - Header

private struct MCPServerDetailHeader: View {
    let name: String
    let transport: MCPTransport
    let enabled: Bool
    let connected: Bool
    let error: String?
    let isManaged: Bool
    let loading: Bool
    let statusText: LocalizedStringResource
    let enabledBinding: Binding<Bool>

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            MCPStatusDot(color: MCPStatusStyle.color(enabled: enabled,
                                                     connected: connected,
                                                     error: error))
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 8) {
                    nameLabel
                        .font(.title3.weight(.semibold))
                        .lineLimit(1)
                    MCPTransportBadge(transport: transport)
                    if isManaged {
                        MCPManagedBadge()
                    }
                }
                Text(statusText)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
            if !isManaged {
                Toggle("Enabled", isOn: enabledBinding)
                    .labelsHidden()
                    .toggleStyle(.switch)
                    .disabled(loading)
                    .help(enabled ? "Disable this server" : "Enable this server")
            }
        }
        .padding(16)
    }

    /// Title text: a localized placeholder for an unnamed server, otherwise the
    /// server's own name (runtime data). Returning `Text` keeps the literal on
    /// the localizable path instead of folding it into a verbatim `String`.
    private var nameLabel: Text {
        name.isEmpty ? Text("Untitled server") : Text(name)
    }
}

// MARK: - Content

private struct MCPServerDetailContent: View {
    @Binding var form: MCPServerForm?
    let isManaged: Bool
    let errorMessage: String?
    let tools: [HostedMCPTool]
    let status: HostedMCPServerStatus?
    let store: MCPServersStore
    let serverName: String
    let onToggle: (HostedMCPTool, Bool) -> Void

    var body: some View {
        Form {
            if isManaged {
                MCPServerManagedBanner()
            }

            MCPServerFormFields(form: Binding(
                get: { form ?? MCPServerForm() },
                set: { form = $0 }
            ), showsEnabled: false)
                .disabled(isManaged)

            if let errorMessage {
                Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                    .font(.caption)
                    .foregroundStyle(.orange)
                    .fixedSize(horizontal: false, vertical: true)
            }

            MCPServerToolsSection(
                store: store,
                serverName: serverName,
                tools: tools,
                disabledTools: form?.disabledTools,
                status: status,
                isManaged: isManaged,
                onToggle: onToggle
            )
        }
        .formStyle(.grouped)
    }
}

// MARK: - Footer

private struct MCPServerDetailFooter: View {
    let isManaged: Bool
    let isSaving: Bool
    let saveConfirmation: String?
    let isSavingTools: Bool
    let hasForm: Bool
    let onDelete: () -> Void
    let onClose: () -> Void
    let onSave: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            if isManaged {
                Text("Managed in Memory")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            } else {
                Button("Delete", role: .destructive, action: onDelete)
                    .disabled(isSaving || isSavingTools)
            }
            if let saveConfirmation {
                Text(saveConfirmation)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            Button("Close", action: onClose)
                .keyboardShortcut(.cancelAction)
            if !isManaged {
                Button(action: onSave) {
                    if isSaving {
                        ProgressView().controlSize(.small)
                    } else {
                        Text("Save")
                    }
                }
                .keyboardShortcut(.defaultAction)
                .disabled(isSaving || !hasForm || isSavingTools)
            }
        }
        .padding(16)
    }
}
