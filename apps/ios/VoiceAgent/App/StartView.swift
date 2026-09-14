import LiveKitComponents
import SwiftUI

/// The initial view shown inside the Conversation destination when the app is
/// not connected to the server.
///
/// The agent is chosen on the first level (the picker) and fixed for this
/// second level, so the old in-screen picker and the settings/QR top-bar
/// buttons live in `AppShellView` instead. This screen keeps the connect
/// affordance, the audio options, and the selected agent's memories entry.
struct StartView: View {
    @EnvironmentObject private var controller: MemorySessionController
    @EnvironmentObject private var store: MemoryStore

    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    @Namespace private var button

    @State private var audioOptionsPresented = false
    @State private var memoriesPresented = false

    var body: some View {
        ZStack(alignment: .topTrailing) {
            VStack(spacing: 8 * .grid) {
                bars()
                if store.capability == .available {
                    memoriesButton()
                        .transition(.opacity)
                }
                connectButton()
                audioOptionsButton()
            }
            .animation(.default, value: store.capability)
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
        .font(.system(size: 12))
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
            HStack {
                Spacer()
                Text("connect.start")
                    .matchedGeometryEffect(id: "connect", in: button)
                Spacer()
            }
            .frame(width: 58 * .grid, height: 11 * .grid)
        } busyLabel: {
            HStack(spacing: 4 * .grid) {
                Spacer()
                Spinner()
                    .transition(.scale.combined(with: .opacity))
                Text("connect.connecting")
                    .matchedGeometryEffect(id: "connect", in: button)
                Spacer()
            }
            .frame(width: 58 * .grid, height: 11 * .grid)
        }
        #if os(visionOS)
        .buttonStyle(.borderedProminent)
        .controlSize(.extraLarge)
        #else
        .buttonStyle(ProminentButtonStyle())
        #endif
    }

    private func audioOptionsButton() -> some View {
        Button {
            audioOptionsPresented = true
        } label: {
            HStack(spacing: .grid) {
                Image(systemName: "slider.horizontal.3")
                Text("audio.title")
            }
            .font(.system(size: 13))
            .foregroundStyle(.fg3)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .popover(isPresented: $audioOptionsPresented) {
            AudioOptionsSheet()
        }
    }

    /// Opens the selected agent's memories (only shown when the agent is
    /// memory-capable, e.g. Diane; Memory has no memory).
    private func memoriesButton() -> some View {
        Button {
            memoriesPresented = true
        } label: {
            HStack(spacing: .grid) {
                Image(systemName: "text.book.closed")
                Text("memories.title")
            }
            .font(.system(size: 13))
            .foregroundStyle(.fg3)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("memories.title")
        .sheet(isPresented: $memoriesPresented) {
            MemoriesView()
        }
    }
}

#Preview {
    StartView()
        .environmentObject(MemorySessionController(config: MemoryConfig()))
        .environmentObject(MemoryStore())
}
