import LiveKit
import LiveKitComponents
import SwiftUI

// MARK: - Controls

/// Mic (voice only) and hang-up for the room. Hang-up ends the session and
/// dismisses back to Chats.
struct ChatSessionControls: View {
    let mode: ChatMode
    let onHangUp: () async -> Void

    @EnvironmentObject private var session: Session
    @EnvironmentObject private var localMedia: LocalMedia

    var body: some View {
        if mode == .voice {
            HStack(spacing: 4 * .grid) {
                microphoneButton
                hangUpButton
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 2 * .grid)
            .background(.bg1)
        } else {
            HStack {
                Spacer()
                hangUpButton
            }
            .padding(.horizontal, 6 * .grid)
            .padding(.top, 1 * .grid)
        }
    }

    private var microphoneButton: some View {
        AsyncButton {
            await localMedia.toggleMicrophone()
            TraceLog.log("mic_toggled", ["on": localMedia.isMicrophoneEnabled])
        } label: {
            Image(systemName: localMedia.isMicrophoneEnabled ? "microphone.fill" : "microphone.slash.fill")
                .frame(width: 16 * .grid, height: 11 * .grid)
                .contentShape(Rectangle())
        }
        .buttonStyle(
            ControlBarButtonStyle(
                isToggled: localMedia.isMicrophoneEnabled,
                foregroundColor: .fg1,
                backgroundColor: .bg2,
                borderColor: .separator1
            )
        )
        .accessibilityLabel("controlBar.microphone")
        .accessibilityValue(localMedia.isMicrophoneEnabled ? Text("controlBar.on") : Text("controlBar.off"))
    }

    private var hangUpButton: some View {
        AsyncButton {
            await onHangUp()
        } label: {
            Image(systemName: "phone.down.fill")
                .frame(width: 12 * .grid, height: 11 * .grid)
                .contentShape(Rectangle())
        }
        .buttonStyle(
            ControlBarButtonStyle(
                foregroundColor: .fgSerious,
                backgroundColor: .bgSerious,
                borderColor: .separatorSerious,
                isDestructive: true
            )
        )
        .disabled(!session.isConnected)
        .accessibilityLabel("chat.disconnect")
        .accessibilityHint("chat.disconnect.hint")
    }
}
