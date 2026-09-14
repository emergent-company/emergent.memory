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
    /// (still persisted under `alfred.agentName`).
    @StateObject private var agentStore: AgentStore
    /// Memory capability + list state for the selected agent.
    @StateObject private var memoryStore: MemoryStore

    init() {
        _agentStore = StateObject(wrappedValue: AgentStore())
        _memoryStore = StateObject(wrappedValue: MemoryStore())
    }

    var body: some Scene {
        WindowGroup {
            AgentSessionRoot()
                .environmentObject(agentStore)
                .environmentObject(memoryStore)
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
/// Also re-checks memory capability for the newly selected agent.
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
    @EnvironmentObject private var memoryStore: MemoryStore

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
            install()
            memoryStore.refreshCapability(agent: agentStore.selectedAgentName)
        }
        .onChange(of: agentStore.selectedAgentName) { _, _ in
            install()
            memoryStore.refreshCapability(agent: agentStore.selectedAgentName)
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
