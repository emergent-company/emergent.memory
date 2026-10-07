import LiveKit
import SwiftUI

/// The agent this app talks to. Dispatched by name through the token
/// endpoint; the picker on the start screen chooses which one.
enum AgentToConnect {
    /// Memory's self-hosted agent, reached through Memory's token endpoint.
    case memory(config: MemoryConfig)

    /// The active configuration (read from `UserDefaults` with build defaults).
    static var current: Self {
        .memory(config: MemoryConfig())
    }

    var tokenSource: any TokenSourceConfigurable {
        switch self {
        case let .memory(config):
            MemoryTokenSource(config: config)
        }
    }
}

@main
struct VoiceAgentApp: App {
    /// The agent list + selection, fetched from the control plane. Replaces
    /// the hardcoded `AgentSelection` list as the selection source of truth
    /// (persisted per project under `alfred.agentName.<projectID>`).
    @StateObject private var agentStore: AgentStore
    /// The onboarded project connections + active selection.
    @StateObject private var projectStore: MemoryProjectStore

    init() {
        _agentStore = StateObject(wrappedValue: AgentStore())
        _projectStore = StateObject(wrappedValue: MemoryProjectStore())
    }

    var body: some Scene {
        WindowGroup {
            AgentSessionRoot()
                .environmentObject(agentStore)
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
/// controller (and its `Session`/`LocalMedia`) is rebuilt from a fresh config
/// on every selection change instead of being mutated in place.
///
/// The controller is injected into the whole shell, so an active voice session
/// survives navigation between the picker and an agent's second level — it is
/// only torn down when the selected agent actually changes.
private struct AgentSessionRoot: View {
    @EnvironmentObject private var agentStore: AgentStore
    @EnvironmentObject private var projectStore: MemoryProjectStore

    @State private var controller: MemorySessionController?
    @State private var audioOptions: AudioOptions?
    /// Navigation path for the level-1 agents stack. Owned here (not in
    /// `AppShellView`) so recreating the controller/shell on agent change
    /// cannot reset it and strand the user on the picker.
    @State private var path: [AppRoute] = []

    var body: some View {
        Group {
            if let controller, let audioOptions {
                AppShellView(path: $path)
                    .environmentObject(controller)
                    .environmentObject(controller.session)
                    .environmentObject(controller.localMedia)
                    .environmentObject(audioOptions)
                    .environment(\.voiceEnabled, true)
                    .environment(\.videoEnabled, false)
                    .environment(\.textEnabled, true)
            }
        }
        .onAppear {
            // Load the active project's remembered agent before building the
            // controller, so the first connect uses the right agent.
            agentStore.setActiveProject(projectStore.activeProjectID)
            install()
        }
        .onChange(of: agentStore.selectedAgentName) { _, _ in
            install()
        }
        .onChange(of: projectStore.activeProjectID) { _, _ in
            // Active project changed: swap the remembered agent for that
            // project. `setActiveProject` publishing a new selection triggers
            // `install()` via the change above; a same-named agent still needs
            // a rebuild for the new connection, which `.memoryConfigChanged`
            // already drives.
            agentStore.setActiveProject(projectStore.activeProjectID)
        }
        .onReceive(NotificationCenter.default.publisher(for: .memoryConfigChanged)) { _ in
            install()
        }
    }

    /// Builds a fresh controller from the selected agent's configuration.
    /// `MemorySessionController` reads its config once, so a new instance is
    /// required for a new agent to take effect.
    private func install() {
        var config = MemoryConfig()
        config.agentName = agentStore.selectedAgentName
        let controller = MemorySessionController(config: config)
        self.controller = controller
        audioOptions = AudioOptions(localMedia: controller.localMedia)
    }
}
