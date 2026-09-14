import SwiftUI

#if os(visionOS)
    /// A platform-specific view that shows all interaction controls with optional chat.
    struct VisionInteractionView: View {
        var chat: Bool
        @FocusState.Binding var keyboardFocus: Bool

        @State private var messageText = ""

        var body: some View {
            HStack {
                participants().rotation3DEffect(.degrees(30), axis: .y, anchor: .trailing)
                agent()
                chatView().rotation3DEffect(.degrees(-30), axis: .y, anchor: .leading)
            }
        }

        private func participants() -> some View {
            VStack {
                Spacer()
                ScreenShareView()
                LocalParticipantView()
                Spacer()
            }
            .frame(width: 125 * .grid)
        }

        private func agent() -> some View {
            AgentView()
                .frame(width: 175 * .grid)
                .frame(maxHeight: .infinity)
                .glassBackgroundEffect()
        }

        private func chatView() -> some View {
            VStack {
                if chat {
                    ChatView(onSuggestion: { prompt in
                        messageText = prompt
                        keyboardFocus = true
                    })
                    ChatInputView(keyboardFocus: _keyboardFocus, text: $messageText)
                }
            }
            .frame(width: 125 * .grid)
            .glassBackgroundEffect()
        }
    }
#endif
