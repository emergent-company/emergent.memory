import LiveKitComponents
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

    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Namespace private var button

    @State private var audioOptionsPresented = false

    var body: some View {
        ZStack(alignment: .topTrailing) {
            VStack(spacing: 8 * .grid) {
                bars()
                if let agent = agentStore.selectedAgent {
                    objectsLink(agent: agent)
                        .transition(.opacity)
                }
                connectButton()
                audioOptionsButton()
            }
            .animation(reduceMotion ? nil : .default, value: agentStore.selectedAgentName)
            .padding(.horizontal, horizontalSizeClass == .regular ? 32 * .grid : 16 * .grid)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .safeAreaInset(edge: .bottom, content: tip)
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

    @ViewBuilder
    private func connectButton() -> some View {
        AsyncButton {
            TraceLog.log("connect_tapped")
            await controller.start()
        } label: {
            connectButtonSizing {
                HStack {
                    Spacer()
                    Text("connect.start")
                        .matchedGeometryEffect(id: "connect", in: button)
                    Spacer()
                }
            }
        } busyLabel: {
            connectButtonSizing {
                HStack(spacing: 4 * .grid) {
                    Spacer()
                    Spinner(tint: .white)
                        .transition(reduceMotion ? .opacity : .scale.combined(with: .opacity))
                    Text("connect.connecting")
                        .matchedGeometryEffect(id: "connect", in: button)
                    Spacer()
                }
            }
        }
        #if os(visionOS)
        .buttonStyle(.borderedProminent)
        .controlSize(.extraLarge)
        #else
        .buttonStyle(ProminentButtonStyle())
        #endif
    }

    /// The connect CTA's footprint: a 58×11-grid pill at default text sizes.
    /// At accessibility sizes the width becomes flexible so the label can use
    /// the whole row, and the height is only a minimum so the label grows the
    /// button instead of being clipped by a fixed box (#1948).
    private func connectButtonSizing(@ViewBuilder _ label: () -> some View) -> some View {
        label()
            .frame(width: dynamicTypeSize.isAccessibilitySize ? nil : 58 * .grid)
            .frame(minHeight: 11 * .grid)
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

#Preview {
    StartView()
        .environmentObject(MemorySessionController(config: MemoryConfig()))
        .environment(AgentStore())
}
