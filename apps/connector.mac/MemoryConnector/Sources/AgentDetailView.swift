import SwiftUI

// The dashboard's agent list and this sheet both key off the definition's `id`,
// so it can act as the sheet's identity.
extension AgentDefinitionSummary: Identifiable {}

/// Read-only detail sheet for one agent definition, opened from the Dashboard
/// "Available Agents" card.
///
/// The summary from the list endpoint carries no tool whitelist, so the sheet
/// fetches the FULL definition on open via ``AgentDetailStore`` and renders its
/// loading/error states. Settings render immediately from the summary and are
/// upgraded with the detail once it arrives. v1 does not edit.
struct AgentDetailView: View {
    let agent: AgentDefinitionSummary
    let projectID: String
    let serverURL: String

    @EnvironmentObject private var accountStore: AccountStore
    @StateObject private var store = AgentDetailStore()

    @SwiftUI.Environment(\.dismiss) private var dismiss

    /// Label column width, matching `ConnectorInfoRow` so all settings rows
    /// align regardless of which row type is used.
    private let labelWidth: CGFloat = 130

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    settingsCard
                    toolsCard
                }
                .padding(24)
                .frame(maxWidth: 620, alignment: .leading)
                .frame(maxWidth: .infinity, alignment: .center)
            }
            .navigationTitle(displayName)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
        .frame(minWidth: 560, minHeight: 480)
        .task(id: loadKey) { await load() }
    }

    // MARK: - Loading

    private struct LoadKey: Equatable {
        let agentID: String
        let projectID: String
        let serverURL: String
        let signedIn: Bool
    }

    private var loadKey: LoadKey {
        LoadKey(agentID: agent.id,
                projectID: projectID,
                serverURL: serverURL,
                signedIn: accountStore.isEffectivelySignedIn)
    }

    private func load() async {
        let project = projectID.trimmingCharacters(in: .whitespacesAndNewlines)
        let server = serverURL.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !project.isEmpty else {
            store.fail("No active project is selected.")
            return
        }
        guard !server.isEmpty else {
            store.fail("No server is configured.")
            return
        }
        do {
            let token = try await accountStore.currentAccessToken()
            await store.load(projectID: project, id: agent.id,
                             serverURL: server, accessToken: token)
        } catch {
            store.fail(error.localizedDescription)
        }
    }

    // MARK: - Settings

    private var settingsCard: some View {
        ConnectorCard(title: "Settings", systemImage: "slider.horizontal.3") {
            VStack(alignment: .leading, spacing: 12) {
                ConnectorInfoRow(label: "Name", value: displayName)

                infoRow("Description") {
                    if let description = agentDescription, !description.isEmpty {
                        Text(description)
                            .font(.subheadline)
                            .fixedSize(horizontal: false, vertical: true)
                            .textSelection(.enabled)
                    } else {
                        Text("—")
                            .font(.subheadline)
                            .foregroundStyle(.tertiary)
                    }
                }

                infoRow("Status") {
                    HStack(spacing: 8) {
                        if let enabled {
                            if enabled {
                                AgentStatusBadge(text: "Enabled", color: .green)
                            } else {
                                AgentStatusBadge(text: "Disabled", color: .secondary)
                            }
                        } else {
                            Text("Not specified")
                                .font(.subheadline)
                                .foregroundStyle(.tertiary)
                        }
                        if isDefault == true {
                            AgentStatusBadge(text: "Default", color: .memoryPrimary)
                        }
                    }
                }

                ConnectorInfoRow(label: "Model", value: modelDisplay)

                if let visibility, !visibility.isEmpty {
                    ConnectorInfoRow(label: "Visibility", value: humanized(visibility))
                }
                if let trigger = triggerType, !trigger.isEmpty {
                    ConnectorInfoRow(label: "Trigger", value: humanized(trigger))
                }
                if let flow = flowType, !flow.isEmpty {
                    ConnectorInfoRow(label: "Flow", value: humanized(flow))
                }

                IdentifierInfoRow(label: "ID", identifier: agent.id)
            }
        }
    }

    /// A settings row whose value wraps (description) or hosts arbitrary views.
    private func infoRow<Content: View>(
        _ label: String,
        @ViewBuilder content: () -> Content
    ) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Text(label)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .frame(width: labelWidth, alignment: .leading)
            content()
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    // MARK: - Tools

    @ViewBuilder
    private var toolsCard: some View {
        ConnectorCard(title: "Tools", systemImage: "wrench.and.screwdriver") {
            switch store.state {
            case .idle, .loading:
                ConnectorLoadingRow(caption: "Loading tools…")
            case .error(let message):
                toolsError(message)
            case .loaded(let detail):
                loadedTools(detail)
            }
        }
    }

    private func loadedTools(_ detail: AgentDefinitionDetail) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            mcpToolsSection(detail.tools)

            if let banned = detail.bannedTools, !banned.isEmpty {
                Divider()
                toolBlock(title: "Banned tools", systemImage: "nosign",
                          names: banned, subtle: true)
            }
            if let skills = detail.skills, !skills.isEmpty {
                Divider()
                toolBlock(title: "Skills", systemImage: "sparkles", names: skills)
            }
            if let native = detail.modelNativeTools, !native.isEmpty {
                Divider()
                toolBlock(title: "Native model tools", systemImage: "cpu", names: native)
            }
            if let workspace = detail.workspaceTools, !workspace.isEmpty {
                Divider()
                toolBlock(title: "Workspace tools", systemImage: "folder", names: workspace)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// The MCP tools whitelist. Empty OR nil both mean "no MCP tools allowed" —
    /// there is no default toolset (only the hidden built-in session-title tool
    /// runs, unless banned).
    @ViewBuilder
    private func mcpToolsSection(_ tools: [String]?) -> some View {
        if let tools, !tools.isEmpty {
            toolBlock(title: "MCP tools", systemImage: "wrench.and.screwdriver", names: tools)
        } else {
            VStack(alignment: .leading, spacing: 4) {
                toolHeader("MCP tools", systemImage: "wrench.and.screwdriver")
                Text("No MCP tools allowed (empty whitelist)")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                Text("Only the built-in session-title tool runs for this agent.")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private func toolBlock(title: String,
                           systemImage: String,
                           names: [String],
                           subtle: Bool = false) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            toolHeader(title, systemImage: systemImage)
            VStack(alignment: .leading, spacing: 0) {
                ForEach(names.indices, id: \.self) { index in
                    if index > names.startIndex { Divider() }
                    toolRow(names[index], systemImage: systemImage, subtle: subtle)
                }
            }
        }
    }

    private func toolHeader(_ title: String, systemImage: String) -> some View {
        HStack(spacing: 6) {
            Image(systemName: systemImage)
                .font(.caption)
            Text(title)
                .font(.caption.weight(.semibold))
        }
        .foregroundStyle(.secondary)
    }

    /// Names are shown verbatim, so relay-style `<instance>_<tool>` ids survive
    /// unmodified.
    private func toolRow(_ name: String, systemImage: String, subtle: Bool) -> some View {
        HStack(spacing: 10) {
            Image(systemName: systemImage)
                .foregroundStyle(subtle ? AnyShapeStyle(.tertiary) : AnyShapeStyle(.secondary))
                .frame(width: 22)
            Text(name)
                .font(.callout)
                .foregroundStyle(subtle ? Color.secondary : Color.primary)
                .strikethrough(subtle, color: .secondary)
                .textSelection(.enabled)
            Spacer(minLength: 0)
        }
        .frame(minHeight: 32)
        .padding(.vertical, 2)
    }

    private func toolsError(_ message: String) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.orange)
            VStack(alignment: .leading, spacing: 4) {
                Text("Couldn't load agent details")
                    .font(.headline)
                Text(message)
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
            Button("Retry") {
                Task { await load() }
            }
        }
    }

    // MARK: - Effective values (detail supersedes the summary once loaded)

    private var detail: AgentDefinitionDetail? {
        if case .loaded(let detail) = store.state { return detail }
        return nil
    }

    private var displayName: String {
        let name = detail?.name ?? agent.name
        return name.isEmpty ? "Untitled agent" : name
    }

    private var agentDescription: String? { detail?.description ?? agent.description }
    private var enabled: Bool? { detail?.enabled ?? agent.enabled }
    private var isDefault: Bool? { detail?.isDefault ?? agent.isDefault }
    private var visibility: String? { detail?.visibility ?? agent.visibility }
    private var triggerType: String? { detail?.triggerType ?? agent.triggerType }
    private var flowType: String? { detail?.flowType ?? agent.flowType }

    /// The server omits the model when the agent has none; show a neutral label.
    private var modelDisplay: String {
        let model = detail?.model ?? agent.model
        if let model, !model.isEmpty { return model }
        return "Default"
    }

    /// Turns `snake_case` / `camelCase` server enums into a readable label.
    private func humanized(_ value: String) -> String {
        let spaced = value
            .replacingOccurrences(of: "_", with: " ")
            .replacingOccurrences(of: "-", with: " ")
        guard let first = spaced.first else { return spaced }
        return first.uppercased() + spaced.dropFirst()
    }
}

/// Small pill describing an agent's enabled / default state, shared by the
/// dashboard list and this detail sheet.
struct AgentStatusBadge: View {
    let text: String
    let color: Color

    var body: some View {
        Text(text)
            .font(.caption2.weight(.semibold))
            .foregroundStyle(color)
            .padding(.horizontal, 7)
            .padding(.vertical, 2)
            .background(Capsule().fill(color.opacity(0.15)))
    }
}
