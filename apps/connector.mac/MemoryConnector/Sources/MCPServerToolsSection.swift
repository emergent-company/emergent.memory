import SwiftUI

// MARK: - Tool selection

/// Pure tool-selection helpers used by a server's detail view: which of its
/// tools are shared with Memory, and how a share toggle edits the
/// `disabled_tools` list. Extracted so the detail view stays small and the
/// rules are easy to test.
enum MCPServerToolSelection {
    /// The name to show for a tool: the server's own, un-namespaced name, which
    /// is what `disabled_tools` stores and what a toggle adds or removes.
    static func displayName(for tool: HostedMCPTool, server: String) -> String {
        MCPServersStore.ownToolName(tool.name, server: server)
    }

    /// The server-local names currently listed in `disabled_tools`.
    static func disabledNames(_ disabledTools: [String]?) -> Set<String> {
        Set(disabledTools ?? [])
    }

    /// Whether a tool is shared (`disabled_tools` does not list its own name).
    static func isShared(_ tool: HostedMCPTool, server: String, disabledTools: [String]?) -> Bool {
        !disabledNames(disabledTools).contains(displayName(for: tool, server: server))
    }

    /// How many of `tools` are currently shared.
    static func sharedCount(tools: [HostedMCPTool], server: String, disabledTools: [String]?) -> Int {
        tools.reduce(0) { $0 + (isShared($1, server: server, disabledTools: disabledTools) ? 1 : 0) }
    }

    /// Appends a row for every configured `disabled_tools` entry the live tools
    /// endpoint no longer reports (disabled tools are filtered out of the
    /// discovered list), so a tool switched off can always be switched back on.
    static func displayTools(discovered: [HostedMCPTool],
                             disabledTools: [String]?,
                             server: String) -> [HostedMCPTool]
    {
        var result = discovered
        let present = Set(result.map { displayName(for: $0, server: server) })
        for own in disabledNames(disabledTools).sorted() where !present.contains(own) {
            result.append(HostedMCPTool(name: "\(server)_\(own)", description: "", inputSchema: nil))
        }
        return result
    }

    /// The updated `disabled_tools` after sharing (`shared == true`) or
    /// unsharing one of a server's own tools.
    static func applying(own: String, shared: Bool, to disabledTools: [String]?) -> [String]? {
        var list = disabledTools ?? []
        if shared {
            list.removeAll { $0 == own }
        } else if !list.contains(own) {
            list.append(own)
        }
        return list.isEmpty ? nil : list
    }
}

// MARK: - Tools section

/// The "Discovered Tools" section of a server's detail view: one sharing toggle
/// per tool. A Memory-managed server is read-only locally, so its toggles are
/// held disabled.
struct MCPServerToolsSection: View {
    let store: MCPServersStore
    let serverName: String
    let tools: [HostedMCPTool]
    let disabledTools: [String]?
    let status: HostedMCPServerStatus?
    let isManaged: Bool
    let onToggle: (HostedMCPTool, Bool) -> Void

    var body: some View {
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
                        displayName: MCPServerToolSelection.displayName(for: tool, server: serverName),
                        isShared: MCPServerToolSelection.isShared(tool,
                                                                  server: serverName,
                                                                  disabledTools: disabledTools),
                        isSaving: store.isSavingTool(server: serverName, tool: tool.name),
                        isBusy: store.isSavingTools(server: serverName) || isManaged,
                        isManaged: isManaged,
                        onToggle: { onToggle(tool, $0) }
                    )
                }
            }
        } header: {
            HStack {
                Text("Discovered Tools")
                Spacer()
                if store.isSavingTools(server: serverName) {
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
            Text(footerText)
        }
    }

    private var sharedCount: Int {
        MCPServerToolSelection.sharedCount(tools: tools,
                                           server: serverName,
                                           disabledTools: disabledTools)
    }

    private var footerText: String {
        if isManaged {
            return "This server is managed in Memory, so its tools can't be shared or unshared here."
        }
        return "Choose which of this server's tools are shared with Memory. Unshared tools stay on this Mac and are never sent to Memory."
    }
}

// MARK: - Tool sharing row

/// One row in the detail view's tool list: a tool with a per-server sharing
/// toggle. `isShared == false` means the tool's name is in the server's
/// `disabled_tools` list, so it is not registered with Memory.
struct MCPToolShareRow: View {
    let tool: HostedMCPTool
    let displayName: String
    let isShared: Bool
    let isSaving: Bool
    /// True while any sharing write for this server is in flight, or when the
    /// server is managed; the toggle is held disabled so it can't be changed
    /// from here.
    let isBusy: Bool
    let isManaged: Bool
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
                if !isShared, !isManaged {
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
                .help(toggleHelp)
        }
        .padding(.vertical, 2)
    }

    private var toggleHelp: String {
        if isManaged {
            return "This server is managed in Memory, so tool sharing can't be changed here."
        }
        return isShared ? "Stop sharing \(displayName) with Memory"
            : "Share \(displayName) with Memory"
    }
}

// MARK: - Unavailable state

/// Shown by the detail sheet when the management API did not return the server.
struct MCPServerUnavailableView: View {
    let message: String?
    let onRetry: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.title3)
                .foregroundStyle(.orange)
            Text("Couldn't load this server")
                .font(.headline)
            messageText
                .font(.callout)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            Button("Retry", action: onRetry)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(24)
    }

    /// The server's error text (runtime data) or a localized fallback. Returning
    /// `Text` keeps the fallback literal on the localizable path instead of
    /// folding it into a verbatim `String`.
    private var messageText: Text {
        message.map { Text($0) }
            ?? Text("The connector's management API did not return this server.")
    }
}

// MARK: - Managed banner

/// Read-only notice at the top of a Memory-managed server's detail form.
struct MCPServerManagedBanner: View {
    var body: some View {
        Section {
            Label {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Managed in Memory")
                    Text("This server is configured in Memory. Its settings can't be edited or removed here.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            } icon: {
                Image(systemName: "cloud")
            }
        }
    }
}
