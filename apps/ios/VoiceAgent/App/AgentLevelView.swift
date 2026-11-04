import SwiftUI

/// Level 2: the second level for one selected agent.
///
/// A two-tab `TabView` exposes Conversation (default) and Chats. Agent
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

    @Environment(AgentStore.self) private var agentStore
    @Environment(\.dismiss) private var dismiss

    /// The agent this level is scoped to, resolved live from the store.
    private var agent: Agent? {
        agentStore.agents.first { $0.name == agentName }
    }

    var body: some View {
        VStack(spacing: 0) {
            TabView {
                conversationTab
                chatsTab
            }
            // Match the level-1 tab bar's opaque background so pushing into an
            // agent does not flash a translucent bar.
            .toolbarBackground(.bg1, for: .tabBar)
        }
        .toolbar(.hidden, for: .tabBar)
        // The custom ScreenHeader chevron is this screen's only back
        // affordance. Hiding the system navigation bar (and with it the system
        // back button) prevents the push from showing two back buttons.
        #if os(iOS)
            .toolbar(.hidden, for: .navigationBar)
            .navigationBarBackButtonHidden(true)
        #endif
            .background(.bg1)
            .onAppear { Log.nav.info("AgentLevelView appear \(agentName)") }
    }

    private var subtitle: LocalizedStringKey {
        guard agent != nil else { return "agents.notFound" }
        return "agents.subtitle"
    }

    /// The shared agent header. Each tab carries it, so the Chats tab can show
    /// its own title and actions without stacking a second header under this
    /// one.
    private var header: some View {
        ScreenHeader(
            title: Text(agent?.name ?? agentName),
            subtitle: Text(subtitle)
        ) {
            dismiss()
        }
    }

    // MARK: - Tabs

    /// Conversation (default tab): the existing connect flow, unchanged —
    /// session state is owned by the root controller, so it survives
    /// switching away and back.
    private var conversationTab: some View {
        VStack(spacing: 0) {
            header
            AppView()
        }
        .tabItem { Label("agents.level.conversation", systemImage: "waveform") }
    }

    /// Chats: every conversation for this agent. Rows push a full chat room
    /// (manual chats) or the read-only transcript (scheduled/board sessions)
    /// onto the outer navigation stack (the one owned by `AppShellView`); no
    /// nested stack, which would corrupt the outer navigation state.
    private var chatsTab: some View {
        ChatsView(agentName: agentName, agentID: agent?.id ?? "")
            .onAppear { Log.nav.info("chats tab appear") }
            .tabItem { Label("chats.title", systemImage: "bubble.left.and.bubble.right") }
    }
}
