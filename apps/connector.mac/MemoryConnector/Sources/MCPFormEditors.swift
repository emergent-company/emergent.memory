import SwiftUI

// MARK: - Form editors

//
// Transport-aware editors used by the add/edit MCP server forms. Extracted from
// the form view file so each stays within the SwiftLint file-length budget.

/// Editable list of key/value rows. Values are masked by default; the reveal
/// control is per row so secret values (auth headers, API keys) are never shown
/// unless the user asks.
struct MCPKeyValueEditor: View {
    let title: LocalizedStringResource
    let keyPlaceholder: String
    let addLabel: LocalizedStringResource
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
                    .accessibilityLabel("Remove row")
                    .help("Remove row")
                }
            }
            Button {
                entries.append(MCPServerForm.Entry())
            } label: {
                Label {
                    Text(addLabel)
                } icon: {
                    Image(systemName: "plus.circle")
                }
            }
            .buttonStyle(.borderless)
            .font(.caption)
        }
        .padding(.vertical, 2)
    }
}

/// Editable list of positional arguments (stdio servers).
struct MCPArgsEditor: View {
    @Binding var args: [MCPServerForm.Arg]

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if !args.isEmpty {
                Text("Arguments")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            ForEach($args) { $arg in
                HStack(spacing: 6) {
                    TextField("Argument", text: $arg.value)
                        .autocorrectionDisabled()
                    Button {
                        args.removeAll { $0.id == arg.id }
                    } label: {
                        Image(systemName: "minus.circle")
                    }
                    .buttonStyle(.borderless)
                    .accessibilityLabel("Remove argument")
                    .help("Remove argument")
                }
            }
            Button {
                args.append(MCPServerForm.Arg())
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
            .accessibilityLabel(revealed ? Text("Hide value") : Text("Reveal value"))
            .help(revealed ? "Hide value" : "Reveal value")
        }
    }
}
