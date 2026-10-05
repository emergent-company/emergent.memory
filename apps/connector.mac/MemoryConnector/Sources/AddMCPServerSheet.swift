import SwiftUI

// MARK: - Form model

/// Editable form state for creating/editing a hosted MCP server.
///
/// Pure data + validation (no view concerns) so the transport-specific rules —
/// name required, command required for stdio, url required for http/sse, and a
/// duplicate-name check — are unit-testable.
struct MCPServerForm: Equatable {
    /// One editable key/value pair (env var or header).
    struct Entry: Identifiable, Equatable {
        var id = UUID()
        var key = ""
        var value = ""
    }

    var name = ""
    var transport: MCPTransport = .stdio
    var enabled = true
    var command = ""
    var url = ""
    var args: [String] = []
    var env: [Entry] = []
    var headers: [Entry] = []
    /// Preserved across an edit so replacing a server's config does not drop
    /// its per-server tool deny list.
    var disabledTools: [String]?

    init() {}

    init(config: HostedMCPServerConfig) {
        name = config.name
        transport = config.transport
        enabled = config.enabled
        command = config.command ?? ""
        url = config.url ?? ""
        args = config.args ?? []
        env = Self.entries(from: config.env)
        headers = Self.entries(from: config.headers)
        disabledTools = config.disabledTools
    }

    private static func entries(from dictionary: [String: String]?) -> [Entry] {
        (dictionary ?? [:])
            .map { Entry(key: $0.key, value: $0.value) }
            .sorted { $0.key < $1.key }
    }

    // MARK: Validation

    var trimmedName: String {
        name.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var trimmedCommand: String {
        command.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var trimmedURL: String {
        url.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// First blocking validation problem, or nil when the form may be submitted.
    /// `existingNames` triggers the duplicate-name check (omit the server's own
    /// name when editing).
    func validationMessage(existingNames: Set<String> = []) -> String? {
        if trimmedName.isEmpty {
            return "Name is required."
        }
        if existingNames.contains(trimmedName) {
            return "A server named “\(trimmedName)” already exists."
        }
        switch transport {
        case .stdio:
            if trimmedCommand.isEmpty {
                return "Command is required for a stdio server."
            }
        case .http, .sse:
            if trimmedURL.isEmpty {
                return "URL is required for an \(transport.rawValue) server."
            }
        }
        return nil
    }

    var isValid: Bool {
        validationMessage() == nil
    }

    // MARK: Payload

    var cleanedArgs: [String]? {
        let cleaned = args
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
        return cleaned.isEmpty ? nil : cleaned
    }

    var envDictionary: [String: String]? {
        Self.dictionary(from: env)
    }

    var headerDictionary: [String: String]? {
        Self.dictionary(from: headers)
    }

    private static func dictionary(from entries: [Entry]) -> [String: String]? {
        var result: [String: String] = [:]
        for entry in entries {
            let key = entry.key.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !key.isEmpty else { continue }
            result[key] = entry.value
        }
        return result.isEmpty ? nil : result
    }

    /// Builds the request payload, including only the fields the selected
    /// transport uses (matching the engine's validation).
    func config() -> HostedMCPServerConfig {
        switch transport {
        case .stdio:
            HostedMCPServerConfig(name: trimmedName,
                                  transport: .stdio,
                                  enabled: enabled,
                                  command: trimmedCommand,
                                  args: cleanedArgs,
                                  env: envDictionary,
                                  disabledTools: disabledTools)
        case .http, .sse:
            HostedMCPServerConfig(name: trimmedName,
                                  transport: transport,
                                  enabled: enabled,
                                  url: trimmedURL,
                                  headers: headerDictionary,
                                  disabledTools: disabledTools)
        }
    }
}

// MARK: - Add sheet

/// Create-a-server sheet: name, transport, the transport-specific connection
/// fields, and an enabled toggle. Validation runs live and blocks Add.
struct AddMCPServerSheet: View {
    @ObservedObject var store: MCPServersStore
    // Qualified: the app declares its own `Environment` type (Sources/Environment.swift),
    // which shadows SwiftUI's, so the property wrapper must be namespaced.
    @SwiftUI.Environment(\.dismiss) private var dismiss

    /// Required/secret metadata surfaced when the sheet is prefilled from the
    /// registry browse flow. `nil` for a manual add.
    let notes: MCPRegistryNotes?

    @State private var form: MCPServerForm
    @State private var errorMessage: String?
    @State private var isSaving = false

    /// Every field stays editable; `prefill` only seeds the starting values.
    init(store: MCPServersStore,
         prefill: MCPServerForm = MCPServerForm(),
         notes: MCPRegistryNotes? = nil)
    {
        _store = ObservedObject(wrappedValue: store)
        self.notes = notes
        _form = State(initialValue: prefill)
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                Image(systemName: "server.rack")
                    .foregroundStyle(.secondary)
                VStack(alignment: .leading, spacing: 2) {
                    Text("Add MCP Server")
                        .font(.headline)
                    if notes != nil {
                        Text("Suggested from the MCP registry")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer()
            }
            .padding(16)

            Divider()

            Form {
                if let notes, !notes.isEmpty {
                    registryNotesSection(notes)
                }
                MCPServerFormFields(form: $form)
            }
            .formStyle(.grouped)

            Divider()
            footer
        }
        .frame(width: 540, height: 580)
    }

    /// Registry context shown above the fields: runner/other warnings and the
    /// required/secret variables the entry declares. Warnings do not block
    /// adding; the user can install a missing runner later.
    private func registryNotesSection(_ notes: MCPRegistryNotes) -> some View {
        Section {
            ForEach(notes.warnings, id: \.self) { warning in
                Label(warning, systemImage: "exclamationmark.triangle.fill")
                    .font(.caption)
                    .foregroundStyle(.orange)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if !notes.requiredEnvLabels.isEmpty {
                registryRequirementRow(label: "Required environment variables",
                                       values: notes.requiredEnvLabels)
            }
            if !notes.requiredHeaderLabels.isEmpty {
                registryRequirementRow(label: "Required headers",
                                       values: notes.requiredHeaderLabels)
            }
        } header: {
            Text("Registry Suggestion")
        } footer: {
            Text("Values from the registry entry are a starting point and can be edited. Secret values are masked and stay on this Mac — they are never sent to Memory.")
        }
    }

    private func registryRequirementRow(label: String, values: [String]) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(values.joined(separator: ", "))
                .font(.caption)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var validationMessage: String? {
        form.validationMessage(existingNames: store.existingNames)
    }

    private var footer: some View {
        HStack(spacing: 12) {
            if let errorMessage {
                Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                    .font(.caption)
                    .foregroundStyle(.orange)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
            } else if let validationMessage {
                Text(validationMessage)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
            Spacer(minLength: 0)
            Button("Cancel") { dismiss() }
                .keyboardShortcut(.cancelAction)
            Button {
                save()
            } label: {
                if isSaving {
                    ProgressView().controlSize(.small)
                } else {
                    Text("Add")
                }
            }
            .keyboardShortcut(.defaultAction)
            .disabled(isSaving || validationMessage != nil)
        }
        .padding(16)
    }

    private func save() {
        guard validationMessage == nil, !isSaving else { return }
        isSaving = true
        errorMessage = nil
        let config = form.config()
        Task {
            let succeeded = await store.create(config)
            isSaving = false
            if succeeded {
                dismiss()
            } else {
                errorMessage = store.errorMessage
            }
        }
    }
}

// MARK: - Shared form fields

/// Transport-aware fields shared by the add sheet and the detail editor.
struct MCPServerFormFields: View {
    @Binding var form: MCPServerForm
    /// The detail view manages the enabled state with its own immediate toggle,
    /// so it hides the field here.
    var showsEnabled = true

    var body: some View {
        Section {
            TextField("Name", text: $form.name, prompt: Text("e.g. filesystem"))
                .autocorrectionDisabled()

            Picker("Transport", selection: $form.transport) {
                ForEach(MCPTransport.allCases) { transport in
                    Text(transport.displayName).tag(transport)
                }
            }
            .pickerStyle(.segmented)

            if showsEnabled {
                Toggle("Enabled", isOn: $form.enabled)
            }
        } header: {
            Text("Server")
        }

        Section {
            switch form.transport {
            case .stdio:
                TextField("Command", text: $form.command,
                          prompt: Text("/usr/local/bin/mcp-server"))
                    .autocorrectionDisabled()
                MCPArgsEditor(args: $form.args)
                MCPKeyValueEditor(title: "Environment Variables",
                                  keyPlaceholder: "NAME",
                                  entries: $form.env)
            case .http, .sse:
                TextField("URL", text: $form.url,
                          prompt: Text("https://example.com/mcp"))
                    .autocorrectionDisabled()
                MCPKeyValueEditor(title: "Headers",
                                  keyPlaceholder: "Authorization",
                                  entries: $form.headers)
            }
        } header: {
            Text(form.transport.usesCommand ? "Process" : "Connection")
        } footer: {
            Text("Environment variables and headers are stored in the connector's local config file in plaintext. They stay on this Mac and are never sent to Memory.")
        }
    }
}
