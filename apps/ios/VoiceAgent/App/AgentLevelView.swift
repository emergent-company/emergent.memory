import SwiftUI

/// Level 2: the second level for one selected agent.
///
/// A two-tab `TabView` exposes Conversation (default) and Sessions. Agent
/// configuration is not supported from the phone, so there is no settings tab.
/// The voice controller lives at the app root (`AgentSessionRoot`), so an
/// active session survives switching tabs and popping back to the picker; the
/// controller is only rebuilt when the selected agent changes.
///
/// The level-1 root tab bar is hidden while this view is pushed
/// (`.toolbar(.hidden, for: .tabBar)`), so only this level's own bottom bar
/// is visible — no double tab bars. The header's back chevron pops back to
/// the picker.
struct AgentLevelView: View {
    let agentName: String

    @EnvironmentObject private var agentStore: AgentStore
    @Environment(\.dismiss) private var dismiss

    /// The agent this level is scoped to, resolved live from the store.
    private var agent: Agent? {
        agentStore.agents.first { $0.name == agentName }
    }

    var body: some View {
        VStack(spacing: 0) {
            header()
            TabView {
                conversationTab
                sessionsTab
            }
        }
        .toolbar(.hidden, for: .tabBar)
        .background(.bg1)
        .onAppear { Log.nav.info("AgentLevelView appear \(agentName)") }
    }

    // MARK: - Header

    private func header() -> some View {
        HStack(spacing: 2 * .grid) {
            Button {
                dismiss()
            } label: {
                Image(systemName: "chevron.left")
                    .font(.system(size: 16, weight: .medium))
                    .foregroundStyle(.fg3)
                    .padding(2 * .grid)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("agents.level.back")

            VStack(alignment: .leading, spacing: 1 * .grid) {
                Text(agent?.name ?? agentName)
                    .font(.system(size: 22, weight: .semibold))
                    .foregroundStyle(.fg0)
                    .lineLimit(1)
                Text(subtitle)
                    .font(.system(size: 12))
                    .foregroundStyle(.fg3)
            }
            Spacer()
        }
        .padding(.horizontal, 4 * .grid)
        .padding(.top, 1 * .grid)
    }

    private var subtitle: LocalizedStringKey {
        guard let agent else { return "agents.notFound" }
        if let flowType = agent.flowType, !flowType.isEmpty {
            return LocalizedStringKey(flowType)
        }
        return "agents.subtitle"
    }

    // MARK: - Tabs

    /// Conversation (default tab): the existing connect flow, unchanged —
    /// session state is owned by the root controller, so it survives
    /// switching away and back.
    private var conversationTab: some View {
        AppView()
            .tabItem { Label("agents.level.conversation", systemImage: "waveform") }
    }

    /// Sessions: rows push their detail onto the outer navigation stack (the
    /// one owned by `AppShellView`); no nested stack, which would corrupt the
    /// outer navigation state.
    private var sessionsTab: some View {
        SessionsView(agentName: agentName)
            .onAppear { Log.nav.info("sessions tab appear") }
            .tabItem { Label("sessions.title", systemImage: "clock") }
    }

}
