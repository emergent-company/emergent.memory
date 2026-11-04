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
    /// Local mirror of the field's text. Keeping the in-flight edit in this
    /// view's own state (with a stable field identity below) stops a parent
    /// re-render — for example the store refresh that follows a Save — from
    /// resetting the control to a stale binding value mid-edit.
    @State private var draft: String

    init(placeholder: String, text: Binding<String>) {
        self.placeholder = placeholder
        _text = text
        _draft = State(initialValue: text.wrappedValue)
    }

    var body: some View {
        HStack(spacing: 4) {
            // Both controls stay mounted and are hidden with opacity rather than
            // swapped in an `if`/`else`: a conditional swap gives the two fields
            // different identities, so re-rendering the row tears the active
            // field down before its edit reaches the binding.
            ZStack(alignment: .leading) {
                TextField(placeholder, text: $draft)
                    .autocorrectionDisabled()
                    .opacity(revealed ? 1 : 0)
                    .allowsHitTesting(revealed)
                    .accessibilityHidden(!revealed)
                SecureField(placeholder, text: $draft)
                    .autocorrectionDisabled()
                    .opacity(revealed ? 0 : 1)
                    .allowsHitTesting(!revealed)
                    .accessibilityHidden(revealed)
            }
            .onChange(of: draft) { _, newValue in
                if text != newValue {
                    text = newValue
                }
            }
            .onChange(of: text) { _, newValue in
                if draft != newValue {
                    draft = newValue
                }
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
