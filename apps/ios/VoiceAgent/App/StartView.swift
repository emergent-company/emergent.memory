import SwiftUI

/// The initial view shown inside the Conversation destination when the app is
/// not connected to the server.
///
/// The agent is chosen on the first level (the picker) and fixed for this
/// second level, so the old in-screen picker and the settings/QR top-bar
/// buttons live in `AppShellView` instead. This screen keeps the connect
/// affordance, the audio options, and the selected agent's objects entry.
struct StartView: View {
    @EnvironmentObject private var controller: MemorySessionController
    @Environment(AgentStore.self) private var agentStore
    /// False when the active project has voice disabled: no call, no audio
    /// options — the server-side setting decides, the app only reflects it.
    @Environment(\.voiceEnabled) private var voiceEnabled

    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var audioOptionsPresented = false

    var body: some View {
        ZStack(alignment: .topTrailing) {
            VStack(spacing: 8 * .grid) {
                bars()
                if let agent = agentStore.selectedAgent {
                    objectsLink(agent: agent)
                        .transition(.opacity)
                }
                if voiceEnabled {
                    connectButton()
                    audioOptionsButton()
                } else {
                    voiceOffNotice()
                }
            }
            .animation(ConnectMotion.selectionAnimation(reduceMotion: reduceMotion), value: agentStore.selectedAgentName)
            .animation(.default, value: voiceEnabled)
            .padding(.horizontal, horizontalSizeClass == .regular ? 32 * .grid : 16 * .grid)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        // Only reserve the tip's inset when there is a tip: with voice off the
        // hint is gone, and an empty inset would leave dead space above the
        // voice-off notice.
        .safeAreaInset(edge: .bottom) {
            if voiceEnabled {
                tip()
            }
        }
    }

    private func bars() -> some View {
        HStack(spacing: .grid) {
            let bars = [2, 8, 12, 8, 2].map { $0 * .grid }
            ForEach(0 ..< 5, id: \.self) { index in
                Rectangle()
                    .fill(.fg0)
                    .frame(width: 2 * .grid, height: bars[index])
            }
        }
    }

    private func tip() -> some View {
        VStack(spacing: 2 * .grid) {
            #if targetEnvironment(simulator)
                Text("connect.simulator")
                    .foregroundStyle(.fgModerate)
            #endif
            Text("connect.tip")
                .foregroundStyle(.fg3)
        }
        .font(MemoryFont.meta)
        .multilineTextAlignment(.center)
        .safeAreaPadding(.horizontal, horizontalSizeClass == .regular ? 32 * .grid : 16 * .grid)
        .safeAreaPadding(.vertical)
    }

    private func connectButton() -> some View {
        ConnectCTA {
            TraceLog.log("connect_tapped")
            await controller.start()
        }
    }

    /// Voice is off for this project, so there is nothing to connect to. The
    /// card explains why the call affordance is missing and where to enable it,
    /// instead of leaving an empty screen.
    private func voiceOffNotice() -> some View {
        VStack(spacing: 2 * .grid) {
            Image(systemName: "speaker.slash")
                .font(.title3)
                .foregroundStyle(.fg3)
                .accessibilityHidden(true)
            Text("connect.voiceOff.title")
                .font(MemoryFont.cardHeader)
                .foregroundStyle(.fg0)
            Text("connect.voiceOff.description")
                .font(MemoryFont.meta)
                .foregroundStyle(.fg3)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(4 * .grid)
        .frame(maxWidth: 80 * .grid)
        .background(.bg2, in: RoundedRectangle(cornerRadius: .cornerRadiusMedium))
        .accessibilityElement(children: .combine)
    }

    private func audioOptionsButton() -> some View {
        Button {
            audioOptionsPresented = true
        } label: {
            HStack(spacing: .grid) {
                Image(systemName: "slider.horizontal.3")
                Text("audio.title")
            }
            .font(.footnote)
            .foregroundStyle(.fg3)
            .frame(minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .popover(isPresented: $audioOptionsPresented) {
            AudioOptionsSheet()
        }
    }

    /// Pushes the selected agent's object browser — the objects it created or
    /// updated. Hidden until the agent list has loaded and an agent is
    /// selected, so there is never an entry point scoped to no agent.
    private func objectsLink(agent: Agent) -> some View {
        NavigationLink(value: AppRoute.objects(ObjectRoute(agentID: agent.id, agentName: agent.name))) {
            HStack(spacing: .grid) {
                Image(systemName: "square.stack.3d.up")
                Text("objects.title")
            }
            .font(.footnote)
            .foregroundStyle(.fg3)
            .frame(minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("objects.title")
    }
}

#Preview("Voice on") {
    StartView()
        .environmentObject(MemorySessionController(config: MemoryConfig()))
        .environment(AgentStore())
}

#Preview("Voice off for the project") {
    StartView()
        .environmentObject(MemorySessionController(config: MemoryConfig()))
        .environment(AgentStore())
        .environment(\.voiceEnabled, false)
}
