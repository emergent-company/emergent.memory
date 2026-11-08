import SwiftUI

@main
struct VoiceAgentApp: App {
    /// The agent list + selection, fetched from the control plane. Replaces
    /// the hardcoded `AgentSelection` list as the selection source of truth
    /// (persisted per project under `alfred.agentName.<projectID>`).
    @State private var agentStore: AgentStore
    /// The onboarded project connections + active selection.
    @StateObject private var projectStore: MemoryProjectStore
    /// Which conversation is open right now (agent + conversation id) and
    /// whether it is showing text or voice. Owned here so the controller, the
    /// shell, and every pushed chat read the same session.
    @State private var activeSession = ActiveSessionStore()

    init() {
        _agentStore = State(initialValue: AgentStore())
        _projectStore = StateObject(wrappedValue: MemoryProjectStore())
    }

    var body: some Scene {
        WindowGroup {
            AgentSessionRoot()
                .environment(agentStore)
                .environment(activeSession)
                .environmentObject(projectStore)
        }
        #if os(macOS)
        .defaultSize(width: 900, height: 900)
        #endif
        #if os(visionOS)
        .windowStyle(.plain)
        .windowResizability(.contentMinSize)
        .defaultSize(width: 1500, height: 500)
        #endif
    }
}

/// Owns the `MemorySessionController` (and the objects derived from it) and
/// recreates them whenever the selected agent changes, so the new agent name
/// reaches the token endpoint on the next connect — no relaunch needed.
///
/// `MemorySessionController` captures its `MemoryConfig` once at init, so the
/// controller (and its `Session`/`LocalMediaModel`) is rebuilt from a fresh config
/// on every selection change instead of being mutated in place. The same
/// applies to opening another conversation: the controller is rebuilt for the
/// new `ActiveSessionStore.launchKey`.
///
/// The controller is injected into the whole shell, so an active voice session
/// survives navigation between the picker and an agent's second level — it is
/// only torn down when the selected agent or the open conversation changes.
private struct AgentSessionRoot: View {
    @Environment(AgentStore.self) private var agentStore
    @Environment(ActiveSessionStore.self) private var activeSession
    @EnvironmentObject private var projectStore: MemoryProjectStore
    @Environment(\.scenePhase) private var scenePhase

    @State private var controller: MemorySessionController?
    @State private var audioOptions: AudioOptions?
    /// The active project's voice enablement, refreshed on launch, project
    /// switch, config change and foreground (see ``ProjectConfigStore``).
    @State private var projectConfig = ProjectConfigStore()
    /// Navigation path for the level-1 agents stack. Owned here (not in
    /// `AppShellView`) so recreating the controller/shell on agent change
    /// cannot reset it and strand the user on the picker.
    @State private var path: [AppRoute] = []
    /// Captures the share extension queued. Uploaded on launch and on every
    /// return to the foreground — the extension cannot do it itself (a file
    /// upload outlives its ~30 s budget).
    @StateObject private var shareInbox = ShareInbox()
    /// The `ActiveSessionStore.launchKey` the installed controller was built
    /// for, so a re-render that does not launch a new session never rebuilds
    /// (and tears down) the connected room.
    @State private var installedKey: String?

    var body: some View {
        Group {
            if let controller, let audioOptions {
                AppShellView(path: $path)
                    .environmentObject(controller)
                    .environmentObject(controller.session)
                    .environmentObject(controller.localMedia)
                    .environment(audioOptions)
                    .environment(controller.chatActivity)
                    .environmentObject(shareInbox)
                    // A project with voice disabled must not offer Start call
                    // or audio options anywhere in the app.
                    .environment(\.voiceEnabled, projectConfig.voiceEnabled)
                    .environment(\.textEnabled, true)
            }
        }
        .onAppear {
            // Load the active project's remembered agent before building the
            // controller, so the first connect uses the right agent.
            agentStore.setActiveProject(projectStore.activeProjectID)
            syncActiveSession()
            install()
            // A share the extension queued uploads as soon as the app is up.
            Task { await shareInbox.drain() }
        }
        // Launch, and every project switch: publish that project's cached
        // voice setting, then refresh it.
        .task(id: projectStore.activeProjectID) {
            await projectConfig.refresh(projectID: projectStore.activeProjectID)
        }
        .onChange(of: agentStore.selectedAgentName) { _, _ in
            syncActiveSession()
            install()
        }
        // A new conversation (or agent) was opened: rebuild the controller so
        // the token request continues the right room. Rebuilds only when the
        // launch key changes, so unrelated re-renders keep the live session.
        .onChange(of: activeSession.launchKey) { _, _ in
            install()
        }
        .onChange(of: projectStore.activeProjectID) { _, _ in
            // Active project changed: swap the remembered agent for that
            // project. The open conversation belongs to the previous project,
            // so it is reset even when the agent name is the same.
            // `setActiveProject` publishing a new selection triggers `install()`
            // via the change above; a same-named agent still needs a rebuild for
            // the new connection, which `.memoryConfigChanged` already drives.
            agentStore.setActiveProject(projectStore.activeProjectID)
            let name = agentStore.selectedAgentName
            if !name.isEmpty {
                activeSession.open(agentName: name, conversationID: nil)
            }
        }
        // A re-scan or credential rotation may land on a different project
        // configuration; the foreground revisit covers a setting changed from
        // the web app while the app was suspended.
        .onChange(of: scenePhase) { _, phase in
            guard phase == .active else { return }
            Task { await projectConfig.refresh(projectID: projectStore.activeProjectID) }
            // Coming back to the app is the retry point for a capture shared
            // while offline, or queued as a file.
            Task { await shareInbox.drain() }
        }
        .onReceive(NotificationCenter.default.publisher(for: .memoryConfigChanged)) { _ in
            install(force: true)
            Task { await projectConfig.refresh(projectID: projectStore.activeProjectID) }
        }
    }

    /// Keeps the open session on the selected agent. Only an actual agent
    /// change resets the conversation: a re-render with the same selection must
    /// never drop an open conversation or its learned id.
    private func syncActiveSession() {
        let name = agentStore.selectedAgentName
        guard !name.isEmpty, activeSession.agentName != name else { return }
        activeSession.open(agentName: name, conversationID: nil)
    }

    /// Builds a fresh controller from the selected agent's configuration and
    /// the conversation the user opened. `MemorySessionController` reads its
    /// config once, so a new instance is required for a new agent or
    /// conversation to take effect.
    ///
    /// The previous controller is ended best-effort, so switching rooms leaves
    /// no stale connection behind.
    private func install(force: Bool = false) {
        let desiredKey = activeSession.launchKey
        guard force || desiredKey != installedKey else { return }
        installedKey = desiredKey

        let previous = controller
        let agentName = activeSession.agentName.isEmpty ? agentStore.selectedAgentName : activeSession.agentName
        var config = MemoryConfig()
        config.agentName = agentName
        let controller = MemorySessionController(
            config: config,
            conversationID: activeSession.launchConversationID,
            startMuted: activeSession.startsMuted
        )
        // Persist the id the worker reports back for a brand-new conversation,
        // so the room can be reopened from the chats list.
        let session = activeSession
        controller.onConversationLearned = { conversationID in
            session.learnConversationID(conversationID)
        }
        self.controller = controller
        audioOptions = AudioOptions(localMedia: controller.localMedia)

        if let previous, previous !== controller {
            Task { await previous.endSession() }
        }
    }
}
