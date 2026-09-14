import LiveKit
import SwiftUI

struct AppView: View {
    @EnvironmentObject private var controller: MemorySessionController
    @EnvironmentObject private var session: Session
    @EnvironmentObject private var localMedia: LocalMedia

    @State private var chat: Bool = false
    @FocusState private var keyboardFocus: Bool
    @Namespace private var namespace

    var body: some View {
        ZStack(alignment: .top) {
            if case .failed = controller.phase {
                start()
            } else if session.isConnected {
                interactions()
            } else {
                start()
            }

            if case let .failed(failure) = controller.phase {
                MemoryFailureView(failure: failure) {
                    Task { await controller.retry() }
                }
                .transition(.move(edge: .top).combined(with: .opacity))
            }

            errors()
        }
        .environment(\.namespace, namespace)
        #if os(visionOS)
            .ornament(attachmentAnchor: .scene(.bottom)) {
                if session.isConnected {
                    ControlBar(chat: $chat)
                        .glassBackgroundEffect()
                }
            }
            .alert(
                localMedia.error?.localizedDescription ?? "error.title",
                isPresented: .constant(localMedia.error != nil)
            ) {
                Button("error.ok") { localMedia.dismissError() }
            }
        #else
            .safeAreaInset(edge: .bottom) {
                    if session.isConnected, !keyboardFocus {
                        ControlBar(chat: $chat)
                            .transition(.asymmetric(
                                insertion: .move(edge: .bottom).combined(with: .opacity),
                                removal: .opacity
                            ))
                    }
                }
        #endif
                .background(.bg1)
                .animation(.default, value: chat)
                .animation(.default, value: session.isConnected)
                .animation(.default, value: session.error?.localizedDescription)
                .animation(.default, value: session.agent.error?.localizedDescription)
                .animation(.default, value: localMedia.isCameraEnabled)
                .animation(.default, value: localMedia.isScreenShareEnabled)
                .animation(.default, value: localMedia.error?.localizedDescription)
        #if os(iOS)
            .sensoryFeedback(.impact, trigger: session.isConnected)
        #endif
    }

    private func start() -> some View {
        StartView()
            .onAppear {
                chat = false
            }
    }

    @ViewBuilder
    private func interactions() -> some View {
        #if os(visionOS)
            VisionInteractionView(chat: chat, keyboardFocus: $keyboardFocus)
                .overlay(alignment: .bottom) {
                    agentListening()
                        .padding(16 * .grid)
                }
        #else
            if chat {
                TextInteractionView(keyboardFocus: $keyboardFocus)
            } else {
                VoiceInteractionView()
                    .overlay(alignment: .bottom) {
                        agentListening()
                            .padding()
                    }
            }
        #endif
    }

    @ViewBuilder
    private func errors() -> some View {
        #if !os(visionOS)
            if let mediaError = localMedia.error {
                ErrorView(error: mediaError) { localMedia.dismissError() }
            }
        #endif
    }

    private func agentListening() -> some View {
        ZStack {
            if session.messages.isEmpty,
               !localMedia.isCameraEnabled,
               !localMedia.isScreenShareEnabled
            {
                Group {
                    if session.agent.isConnected {
                        Text("agent.listening")
                    } else {
                        Text("agent.waiting")
                    }
                }
                .font(.system(size: 15))
                .shimmering()
                .transition(.blurReplace)
            }
        }
        .animation(.default, value: session.messages.isEmpty)
        .animation(.default, value: session.agent.isConnected)
    }
}
