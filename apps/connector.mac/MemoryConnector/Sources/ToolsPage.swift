import SwiftUI

/// Local MCP tools page: per-tool enable toggles. When a project is active the
/// toggles edit that project's stored profile; with no active project they edit
/// the shared defaults used to seed profiles. Either way the engine config is
/// rewritten and the engine restarts through the connected project.
struct ToolsPage: View {
    @EnvironmentObject private var settings: ConnectorSettings
    @EnvironmentObject private var projectStore: ProjectStore
    @State private var alertMessage: String?

    var body: some View {
        Form {
            Section {
                profileScopeRow
                disconnectedHint
                masterToolsRow
                Divider()
                ForEach(ToolCatalog.tools) { tool in
                    Toggle(isOn: binding(for: tool)) {
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            Image(systemName: iconName(for: tool.service))
                                .foregroundStyle(.secondary)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(tool.displayName)
                                Text(tool.summary)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                }
            } header: {
                Text("Local MCP Tools")
            } footer: {
                Text("Enabled tools appear in Memory → Settings → MCP nodes and can be attached to agents. Changes take effect after the engine restarts.")
            }
        }
        .formStyle(.grouped)
        .navigationTitle("MCP Tools")
        .alert("Memory", isPresented: Binding(
            get: { alertMessage != nil },
            set: { if !$0 { alertMessage = nil } }
        )) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(alertMessage ?? "")
        }
    }

    // MARK: - Scope

    private var profileScopeRow: some View {
        HStack(spacing: 8) {
            Image(systemName: projectStore.hasActiveProject ? "shippingbox" : "square.stack.3d.up")
                .foregroundStyle(.secondary)
            Text(profileScopeText)
                .font(.caption)
                .foregroundStyle(.secondary)
            Spacer(minLength: 0)
        }
    }

    private var profileScopeText: String {
        guard projectStore.hasActiveProject else { return "Shared (no project selected)" }
        if let name = projectStore.activeProjectName { return "Applies to \(name)" }
        return "Applies to the active project"
    }

    private func iconName(for service: ToolCatalog.Service) -> String {
        switch service {
        case .notes: return "note.text"
        case .reminders: return "checklist"
        }
    }

    /// Shown while the active project is not the connected one: tool edits are
    /// still allowed and persisted, but take effect on connect.
    @ViewBuilder
    private var disconnectedHint: some View {
        if let id = projectStore.activeProjectID, !projectStore.isConnected(id) {
            Label("Not connected — tool changes are saved and take effect when you connect.",
                  systemImage: "bolt.slash")
                .font(.caption)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    // MARK: - Master control

    private var enabledToolCount: Int {
        ToolCatalog.tools.filter { !effectiveDisabledTools.contains($0.id) }.count
    }

    private var allToolsEnabled: Bool {
        enabledToolCount == ToolCatalog.tools.count
    }

    /// Master state rendered under the switch: all-on / all-off / mixed.
    private var allToolsStateText: String {
        let total = ToolCatalog.tools.count
        let on = enabledToolCount
        if on == 0 { return "All off" }
        if on == total { return "All on" }
        return "Mixed — \(on) of \(total) on"
    }

    private var masterBinding: Binding<Bool> {
        Binding(
            get: { allToolsEnabled },
            set: { enabled in Task { await setAllTools(enabled) } }
        )
    }

    /// Master on/off for every catalog tool: active project profile when one is
    /// selected, otherwise the shared defaults.
    private func setAllTools(_ enabled: Bool) async {
        if let id = projectStore.activeProjectID {
            await projectStore.setAllTools(enabled: enabled, projectID: id)
        } else {
            settings.updateDisabledTools(enabled ? [] : Set(ToolCatalog.tools.map(\.id)))
        }
        switch projectStore.state {
        case .error(let message):
            alertMessage = message
        case .signedOut:
            alertMessage = "Your session expired. Sign in to change tools."
        default:
            break
        }
    }

    private var masterToolsRow: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: "switch.2")
                .foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 2) {
                Text("All local tools")
                    .fontWeight(.semibold)
                Text(allToolsStateText)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            Toggle("", isOn: masterBinding)
                .labelsHidden()
                .toggleStyle(.switch)
        }
    }

    // MARK: - Toggles

    /// Disabled set for the current scope: the active project's profile, or the
    /// shared settings when no project is active.
    private var effectiveDisabledTools: Set<String> {
        projectStore.hasActiveProject ? projectStore.activeDisabledTools : settings.disabledTools
    }

    /// Toggle is "enabled" == tool NOT in the disabled set. The setter persists
    /// to the active project's profile (or shared settings) and restarts.
    private func binding(for tool: ToolCatalog.Tool) -> Binding<Bool> {
        Binding<Bool>(
            get: { !self.effectiveDisabledTools.contains(tool.id) },
            set: { enabled in
                var disabled = self.effectiveDisabledTools
                if enabled {
                    disabled.remove(tool.id)
                } else {
                    disabled.insert(tool.id)
                }
                Task {
                    await projectStore.saveActiveProfile(disabledTools: disabled,
                                                         instanceID: projectStore.activeInstanceID)
                    switch projectStore.state {
                    case .error(let message):
                        alertMessage = message
                    case .signedOut:
                        alertMessage = "Your session expired. Sign in to change tools."
                    default:
                        break
                    }
                }
            }
        )
    }
}
