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

    var trimmedName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    var trimmedCommand: String { command.trimmingCharacters(in: .whitespacesAndNewlines) }
    var trimmedURL: String { url.trimmingCharacters(in: .whitespacesAndNewlines) }

    /// First blocking validation problem, or nil when the form may be submitted.
    /// `existingNames` triggers the duplicate-name check (omit the server's own
    /// name when editing).
    func validationMessage(existingNames: Set<String> = []) -> String? {
        if trimmedName.isEmpty { return "Name is required." }
        if existingNames.contains(trimmedName) {
            return "A server named “\(trimmedName)” already exists."
        }
        switch transport {
        case .stdio:
            if trimmedCommand.isEmpty { return "Command is required for a stdio server." }
        case .http, .sse:
            if trimmedURL.isEmpty { return "URL is required for an \(transport.rawValue) server." }
        }
        return nil
    }

    var isValid: Bool { validationMessage() == nil }

    // MARK: Payload

    var cleanedArgs: [String]? {
        let cleaned = args
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
        return cleaned.isEmpty ? nil : cleaned
    }

    var envDictionary: [String: String]? { Self.dictionary(from: env) }
    var headerDictionary: [String: String]? { Self.dictionary(from: headers) }

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
            return HostedMCPServerConfig(name: trimmedName,
                                         transport: .stdio,
                                         enabled: enabled,
                                         command: trimmedCommand,
                                         args: cleanedArgs,
                                         env: envDictionary,
                                         disabledTools: disabledTools)
        case .http, .sse:
            return HostedMCPServerConfig(name: trimmedName,
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

    @State private var form = MCPServerForm()
    @State private var errorMessage: String?
    @State private var isSaving = false

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                Image(systemName: "server.rack")
                    .foregroundStyle(.secondary)
                Text("Add MCP Server")
                    .font(.headline)
                Spacer()
            }
            .padding(16)

            Divider()

            Form {
                MCPServerFormFields(form: $form)
            }
            .formStyle(.grouped)

            Divider()
            footer
        }
        .frame(width: 540, height: 580)
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

// MARK: - Editors

/// Editable list of key/value rows. Values are masked by default; the reveal
/// control is per row so secret values (auth headers, API keys) are never shown
/// unless the user asks.
struct MCPKeyValueEditor: View {
    let title: String
    let keyPlaceholder: String
    @Binding var entries: [MCPServerForm.Entry]

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if !entries.isEmpty {
                Text(title)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            ForEach($entries) { $entry in
                HStack(spacing: 6) {
                    TextField(keyPlaceholder, text: $entry.key)
                        .autocorrectionDisabled()
                    MCPSecretField(placeholder: "Value", text: $entry.value)
                    Button {
                        entries.removeAll { $0.id == entry.id }
                    } label: {
                        Image(systemName: "minus.circle")
                    }
                    .buttonStyle(.borderless)
                    .help("Remove row")
                }
            }
            Button {
                entries.append(MCPServerForm.Entry())
            } label: {
                Label("Add \(title.lowercased())", systemImage: "plus.circle")
            }
            .buttonStyle(.borderless)
            .font(.caption)
        }
        .padding(.vertical, 2)
    }
}

/// Editable list of positional arguments (stdio servers).
struct MCPArgsEditor: View {
    @Binding var args: [String]

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if !args.isEmpty {
                Text("Arguments")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            ForEach(args.indices, id: \.self) { index in
                HStack(spacing: 6) {
                    TextField("Argument", text: Binding(
                        get: { index < args.count ? args[index] : "" },
                        set: { if index < args.count { args[index] = $0 } }
                    ))
                    .autocorrectionDisabled()
                    Button {
                        if index < args.count { args.remove(at: index) }
                    } label: {
                        Image(systemName: "minus.circle")
                    }
                    .buttonStyle(.borderless)
                    .help("Remove argument")
                }
            }
            Button {
                args.append("")
            } label: {
                Label("Add argument", systemImage: "plus.circle")
            }
            .buttonStyle(.borderless)
            .font(.caption)
        }
        .padding(.vertical, 2)
    }
}

/// A text field that masks its value by default, with a reveal toggle. Used for
/// env vars and headers so secrets are not displayed on screen.
struct MCPSecretField: View {
    let placeholder: String
    @Binding var text: String
    @State private var revealed = false

    var body: some View {
        HStack(spacing: 4) {
            if revealed {
                TextField(placeholder, text: $text)
                    .autocorrectionDisabled()
            } else {
                SecureField(placeholder, text: $text)
                    .autocorrectionDisabled()
            }
            Button {
                revealed.toggle()
            } label: {
                Image(systemName: revealed ? "eye.slash" : "eye")
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.borderless)
            .help(revealed ? "Hide value" : "Reveal value")
        }
    }
}
