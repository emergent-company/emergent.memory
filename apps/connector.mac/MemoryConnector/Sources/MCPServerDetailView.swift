import SwiftUI

/// Detail sheet for one hosted MCP server: edit its config, toggle it on/off,
/// inspect the tools discovered on it, and delete it (with confirmation).
///
/// The enabled toggle applies immediately; other config changes are applied on
/// Save (a full `PUT .../config` replace). A Memory-managed server is read-only
/// here — it is edited in Memory — so its controls are hidden or disabled.
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

    /// True when Memory manages this server: either the row already knew, or
    /// the loaded list/detail does.
    private var isManaged: Bool {
        server.managed || store.isManaged(server.name)
    }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()

            if form == nil, isLoading {
                ConnectorLoadingRow(caption: "Loading server…")
                    .padding()
                Spacer()
            } else if form == nil {
                MCPServerUnavailableView(message: errorMessage) { Task { await reload() } }
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
                            titleVisibility: .visible)
        {
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
                    .disabled(form == nil)
                    .help((form?.enabled ?? server.enabled) ? "Disable this server" : "Enable this server")
            }
        }
        .padding(16)
    }

    private var statusText: String {
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

    // MARK: - Content

    private var content: some View {
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
                serverName: server.name,
                tools: tools,
                disabledTools: form?.disabledTools,
                status: status,
                isManaged: isManaged,
                onToggle: { tool, shared in Task { await setShared(tool, shared) } }
            )
        }
        .formStyle(.grouped)
    }

    // MARK: - Footer

    private var footer: some View {
        HStack(spacing: 12) {
            if isManaged {
                Text("Managed in Memory")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            } else {
                Button("Delete", role: .destructive) { confirmDelete = true }
                    .disabled(isSaving || store.isSavingTools(server: server.name))
            }
            if let saveConfirmation {
                Text(saveConfirmation)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            Button("Close") { dismiss() }
                .keyboardShortcut(.cancelAction)
            if !isManaged {
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
