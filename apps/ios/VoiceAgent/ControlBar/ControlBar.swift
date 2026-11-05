import LiveKitComponents

/// A multiplatform view that shows the control bar: audio and chat controls.
/// Available controls depend on the agent features and the track availability.
/// - SeeAlso: ``AgentFeatures``
struct ControlBar: View {
    @EnvironmentObject private var session: Session
    @EnvironmentObject private var localMedia: LocalMediaModel

    @Binding var chat: Bool
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    @Environment(\.voiceEnabled) private var voiceEnabled
    @Environment(\.textEnabled) private var textEnabled

    @State private var audioOptionsPresented = false

    private enum Constants {
        static let buttonWidth: CGFloat = 16 * .grid
        static let buttonHeight: CGFloat = 11 * .grid
    }

    var body: some View {
        HStack(spacing: .zero) {
            biggerSpacer()
            if voiceEnabled {
                audioControls()
                flexibleSpacer()
            }
            if textEnabled {
                textInputButton()
                flexibleSpacer()
            }
            disconnectButton()
            biggerSpacer()
        }
        .buttonStyle(
            ControlBarButtonStyle(
                foregroundColor: .fg1,
                backgroundColor: .bg2,
                borderColor: .separator1
            )
        )
        .font(.body.weight(.medium))
        .frame(height: 15 * .grid)
        #if !os(visionOS)
            .overlay(
                RoundedRectangle(cornerRadius: .cornerRadiusLarge)
                    .stroke(.separator1, lineWidth: 1)
            )
            .background(
                RoundedRectangle(cornerRadius: .cornerRadiusLarge)
                    .fill(.bg1)
                    .shadow(color: .black.opacity(0.1), radius: 10, y: 10)
            )
            .safeAreaPadding(.bottom, 8 * .grid)
            .safeAreaPadding(.horizontal, 16 * .grid)
        #endif
    }

    private func flexibleSpacer() -> some View {
        Spacer()
            .frame(maxWidth: horizontalSizeClass == .regular ? 8 * .grid : 2 * .grid)
    }

    private func biggerSpacer() -> some View {
        // Cap the outer spacers so the bar never stretches edge-to-edge on wide
        // (iPad) layouts; on phones the content still fills and compresses them.
        Spacer()
            .frame(maxWidth: horizontalSizeClass == .regular ? 8 * .grid : 40 * .grid)
    }

    private func separator() -> some View {
        Rectangle()
            .fill(.separator1)
            .frame(width: 1, height: 3 * .grid)
    }

    private func audioControls() -> some View {
        HStack(spacing: .zero) {
            Spacer()
            AsyncButton {
                await localMedia.toggleMicrophone()
                TraceLog.log("mic_toggled", ["on": localMedia.isMicrophoneEnabled])
            } label: {
                HStack(spacing: .grid) {
                    Image(systemName: localMedia.isMicrophoneEnabled ? "microphone.fill" : "microphone.slash.fill")
                        .transition(.symbolEffect)
                    BarAudioVisualizer(
                        audioTrack: localMedia.microphoneTrack,
                        barColor: .fg1,
                        barCount: 3,
                        barSpacingFactor: 0.1
                    )
                    .frame(width: 2 * .grid, height: 0.5 * Constants.buttonHeight)
                    .frame(maxHeight: .infinity)
                    .id(localMedia.microphoneTrack?.id)
                }
                .frame(height: Constants.buttonHeight)
                .padding(.horizontal, 2 * .grid)
                .contentShape(Rectangle())
            }
            .contextMenu {
                Button("audio.title") { audioOptionsPresented = true }
            }
            .accessibilityLabel("controlBar.microphone")
            .accessibilityValue(localMedia.isMicrophoneEnabled ? Text("controlBar.on") : Text("controlBar.off"))
            .accessibilityHint("controlBar.microphone.hint")
            #if os(macOS)
                separator()
                AudioDeviceSelector()
                    .frame(height: Constants.buttonHeight)
            #else
                // The context menu above is not discoverable on touch platforms,
                // show an explicit affordance for the audio options.
                separator()
                audioOptionsButton()
            #endif
            Spacer()
        }
        .frame(width: Constants.buttonWidth)
        .popover(isPresented: $audioOptionsPresented) {
            AudioOptionsSheet()
        }
    }

    /// Touch-platform affordance for the audio options: the context menu on the
    /// microphone button is not discoverable, so show an explicit chevron.
    private func audioOptionsButton() -> some View {
        Button {
            audioOptionsPresented = true
        } label: {
            Image(systemName: "chevron.up")
                .font(MemoryFont.metaEmphasis)
                .frame(height: Constants.buttonHeight)
                .padding(.horizontal, .grid)
                .contentShape(Rectangle())
        }
        .accessibilityLabel("audio.title")
    }

    private func textInputButton() -> some View {
        Button {
            chat.toggle()
            TraceLog.log(chat ? "chat_mode_entered" : "voice_mode_entered")
        } label: {
            Image(systemName: "text.bubble.fill")
                .frame(width: Constants.buttonWidth, height: Constants.buttonHeight)
                .contentShape(Rectangle())
        }
        .buttonStyle(
            ControlBarButtonStyle(
                isToggled: chat,
                foregroundColor: .fg1,
                backgroundColor: .bg2,
                borderColor: .separator1
            )
        )
        .disabled(!session.agent.isConnected)
        .accessibilityLabel("controlBar.chat")
        .accessibilityValue(chat ? Text("controlBar.on") : Text("controlBar.off"))
        .accessibilityHint("controlBar.chat.hint")
    }

    private func disconnectButton() -> some View {
        AsyncButton {
            TraceLog.log("hangup_tapped")
            await session.end()
        } label: {
            Image(systemName: "phone.down.fill")
                .frame(width: Constants.buttonWidth, height: Constants.buttonHeight)
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
        .accessibilityLabel("controlBar.leave")
        .accessibilityHint("controlBar.leave.hint")
    }
}
