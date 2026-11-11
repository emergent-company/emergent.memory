import SwiftUI

extension ChatSessionView {
    /// The in-call surface, clipped under the header and controls, with the
    /// connecting line over it while the room joins.
    var voiceRoom: some View {
        ZStack {
            VoiceInteractionView()
            if controller.phase == .connecting {
                LoadingStateView(title: "chat.connecting")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(.bg1)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        // The in-call surface ignores the safe area: clip it under header/controls.
        .clipped()
    }
}
