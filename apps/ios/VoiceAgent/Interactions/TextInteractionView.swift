import LiveKit
import SwiftUI

/// A multiplatform view that shows text-specific interaction controls.
///
/// Depending on the track availability, the view will show:
/// - agent participant view
/// - local participant camera preview
/// - local participant screen share preview
///
/// Additionally, the view shows a complete chat view with text input capabilities.
struct TextInteractionView: View {
    @EnvironmentObject private var session: Session
    @EnvironmentObject private var localMedia: LocalMedia
    @EnvironmentObject private var controller: MemorySessionController
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass

    @FocusState.Binding var keyboardFocus: Bool

    /// The shared composer draft (lifted so `ChatView` suggested-prompt chips
    /// can fill it without auto-sending).
    @State private var messageText = ""

    var body: some View {
        VStack {
            VStack {
                participants()
                ChatView(onSuggestion: { prompt in
                    messageText = prompt
                    keyboardFocus = true
                })
                #if os(macOS)
                    .frame(maxWidth: 128 * .grid)
                #else
                    .frame(maxWidth: horizontalSizeClass == .regular ? 128 * .grid : .infinity)
                #endif
                    .blurredTop()
            }
            #if os(iOS)
            .contentShape(Rectangle())
            .onTapGesture {
                keyboardFocus = false
            }
            #endif
            ChatInputView(keyboardFocus: _keyboardFocus, text: $messageText)
        }
    }

    private func participants() -> some View {
        HStack {
            Spacer()
            AgentView()
                .frame(maxWidth: session.agent.avatarVideoTrack != nil ? 50 * .grid : 25 * .grid)
            ScreenShareView()
            LocalParticipantView()
            Spacer()
        }
        .frame(
            height: localMedia.isCameraEnabled || localMedia.isScreenShareEnabled
                || session.agent.avatarVideoTrack != nil ? 50 * .grid : 25 * .grid
        )
        .safeAreaPadding()
    }
}
