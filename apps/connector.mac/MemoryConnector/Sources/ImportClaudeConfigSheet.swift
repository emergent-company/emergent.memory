import SwiftUI
import UniformTypeIdentifiers

/// Import a Claude-format MCP config document into the connector's hosted
/// server set. Supports pasting the document or selecting a file; the parsed
/// result is shown in place so a partial import (some entries skipped or
/// rejected) is actionable without re-running.
struct ImportClaudeConfigSheet: View {
    let store: MCPServersStore
    // Qualified: the app declares its own `Environment` type, shadowing SwiftUI's.
    @SwiftUI.Environment(\.dismiss) private var dismiss

    @State private var text = ""
    @State private var fileName: String?
    @State private var overwrite = false
    @State private var dryRun = false
    @State private var showingImporter = false
    @State private var isSaving = false
    @State private var errorMessage: String?

    private var trimmed: String {
        text.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// True when the textarea holds a non-empty JSON object (the API accepts an
    /// mcpServers document or a single bare server object).
    private var isValidJSON: Bool {
        guard !trimmed.isEmpty, let data = trimmed.data(using: .utf8) else { return false }
        guard let object = try? JSONSerialization.jsonObject(with: data) else { return false }
        return object is [String: Any]
    }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            Form {
                configSection
                optionsSection
                if let result = store.importResult {
                    resultSection(result)
                }
            }
            .formStyle(.grouped)
            Divider()
            footer
        }
        .frame(width: 660, height: 640)
        .onAppear { store.clearImportResult() }
        .fileImporter(isPresented: $showingImporter,
                      allowedContentTypes: [.json],
                      allowsMultipleSelection: false)
        { result in
            loadFile(result)
        }
    }

    private var header: some View {
        HStack(spacing: 10) {
            Image(systemName: "file.json")
                .foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 2) {
                Text("Import from Claude")
                    .font(.headline)
                Text("Add every server from a Claude MCP config to this connector.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
        }
        .padding(16)
    }

    private var configSection: some View {
        Section {
            TextEditor(text: $text)
                .font(.system(.caption, design: .monospaced))
                .frame(minHeight: 220)
                .overlay {
                    if trimmed.isEmpty {
                        Text(#"{"mcpServers": {"github": {"command": "npx", "args": ["-y", "pkg"]}}}"#)
                            .font(.system(.caption, design: .monospaced))
                            .foregroundStyle(.tertiary)
                            .padding(.top, 2)
                            .padding(.leading, 4)
                            .allowsHitTesting(false)
                    }
                }
            HStack(spacing: 10) {
                Button("Choose file…") { showingImporter = true }
                if let fileName {
                    Text(fileName)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                Spacer()
                if !trimmed.isEmpty {
                    Button("Clear") { text = ""; fileName = nil }
                        .buttonStyle(.borderless)
                }
            }
        } header: {
            Text("Claude config")
        } footer: {
            Text("Paste claude_desktop_config.json, .mcp.json, or a single server object. HTTP/SSE servers and local commands are both added. Commands run on this Mac; secrets stay in the connector's local config.")
        }
    }

    private var optionsSection: some View {
        Section {
            Toggle("Overwrite existing servers", isOn: $overwrite)
            Toggle("Dry run — validate without writing", isOn: $dryRun)
        } footer: {
            Text("By default an entry whose name already exists is skipped. Overwriting replaces its connection config but keeps its enabled state and disabled tools.")
        }
    }

    private func resultSection(_ result: HostedMCPImportResult) -> some View {
        Section {
            Text(summary(result))
                .font(.callout.weight(.medium))
            nameList("Created", names: result.created)
            nameList("Updated", names: result.updated)
            if !result.skipped.isEmpty {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Skipped").font(.caption.weight(.semibold))
                    ForEach(result.skipped, id: \.name) { skip in
                        Text("\(skip.name) — \(skip.reason)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            if let warnings = result.warnings, !warnings.isEmpty {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Warnings").font(.caption.weight(.semibold))
                    ForEach(warnings, id: \.name) { warning in
                        Text("\(warning.name) — \(warning.message)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            if !result.failed.isEmpty {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Failed").font(.caption.weight(.semibold)).foregroundStyle(.orange)
                    ForEach(result.failed, id: \.name) { failure in
                        Text("\(failure.name) — \(failure.error)")
                            .font(.caption)
                            .foregroundStyle(.orange)
                    }
                }
            }
        } header: {
            Text("Import result")
        }
    }

    @ViewBuilder
    private func nameList(_ label: LocalizedStringResource, names: [String]) -> some View {
        if !names.isEmpty {
            VStack(alignment: .leading, spacing: 2) {
                Text(label).font(.caption.weight(.semibold))
                ForEach(names, id: \.self) { name in
                    Text(name).font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }

    private var footer: some View {
        HStack(spacing: 12) {
            if let errorMessage {
                Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                    .font(.caption)
                    .foregroundStyle(.orange)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
            } else if !trimmed.isEmpty, !isValidJSON {
                Text("The config is not valid JSON.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            if store.importResult != nil {
                Button("Done") { dismiss() }
                    .keyboardShortcut(.defaultAction)
            } else {
                Button("Cancel") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button {
                    runImport()
                } label: {
                    if isSaving {
                        ProgressView().controlSize(.small)
                    } else {
                        Text(dryRun ? "Validate" : "Import")
                    }
                }
                .keyboardShortcut(.defaultAction)
                .disabled(isSaving || !isValidJSON)
            }
        }
        .padding(16)
    }

    private func runImport() {
        guard isValidJSON, !isSaving else { return }
        isSaving = true
        errorMessage = nil
        let data = Data(trimmed.utf8)
        Task {
            let succeeded = await store.importClaudeConfig(config: data, overwrite: overwrite, dryRun: dryRun)
            isSaving = false
            if !succeeded {
                errorMessage = store.errorMessage
            }
        }
    }

    private func loadFile(_ result: Result<[URL], Error>) {
        do {
            guard let url = try result.get().first else { return }
            let scoped = url.startAccessingSecurityScopedResource()
            defer {
                if scoped {
                    url.stopAccessingSecurityScopedResource()
                }
            }
            let data = try Data(contentsOf: url)
            guard let contents = String(data: data, encoding: .utf8) else {
                errorMessage = "The file is not UTF-8 text."
                return
            }
            text = contents
            fileName = url.lastPathComponent
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func summary(_ result: HostedMCPImportResult) -> String {
        let prefix = result.dryRun ? "Dry run: " : ""
        return "\(prefix)\(result.created.count) created, \(result.updated.count) updated, "
            + "\(result.skipped.count) skipped, \(result.failed.count) failed"
    }
}

/// Toolbar control that opens the Claude import sheet. Extracted from
/// MCPServersPage so that view stays within its type-length budget.
struct ImportClaudeToolbarButton: View {
    let isEnabled: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Label("Import from Claude…", systemImage: "file.json")
        }
        .buttonStyle(.borderless)
        .disabled(!isEnabled)
        .help("Import servers from a Claude MCP config")
    }
}
