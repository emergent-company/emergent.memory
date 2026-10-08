import LiveKitComponents
import SwiftUI

/// A multiplatform view that shows the chat input text field, the send button
/// (which becomes a stop/interrupt button while the agent is replying), and
/// the focused composer text (lifted so suggested-prompt chips can fill it).
struct ChatInputView: View {
    @EnvironmentObject private var session: Session
    @EnvironmentObject private var controller: MemorySessionController
    @Environment(ChatActivityStore.self) private var store

    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    @FocusState.Binding var keyboardFocus: Bool
    @Binding var text: String

    /// Bumped on each send to drive the send haptic.
    @State private var sendFeedback = 0

    init(keyboardFocus: FocusState<Bool>.Binding, text: Binding<String>) {
        _keyboardFocus = keyboardFocus
        _text = text
    }

    var body: some View {
        HStack(alignment: .bottom, spacing: 3 * .grid) {
            textField()
            if store.isGeneratingReply {
                stopButton()
            } else {
                sendButton()
            }
        }
        .frame(minHeight: 12 * .grid)
        .frame(maxWidth: horizontalSizeClass == .regular ? 128 * .grid : 92 * .grid)
        #if !os(visionOS)
            // A single rounded surface carries both the fill and the border, so
            // the stroke is not clipped by the shape mask. `strokeBorder` keeps
            // the hairline inside the corner radius.
            .background(
                RoundedRectangle(cornerRadius: .cornerRadiusLarge)
                    .fill(.bg2)
                    .overlay(
                        RoundedRectangle(cornerRadius: .cornerRadiusLarge)
                            .strokeBorder(.separator1.opacity(0.4), lineWidth: 1)
                    )
            )
        #endif
            .clipShape(RoundedRectangle(cornerRadius: .cornerRadiusLarge))
            .safeAreaPadding(.horizontal, 4 * .grid)
            .safeAreaPadding(.bottom, 4 * .grid)
        #if os(iOS)
            .sensoryFeedback(.impact(weight: .light), trigger: sendFeedback)
        #endif
    }

    @ViewBuilder
    private func textField() -> some View {
        TextField("message.placeholder", text: $text, axis: .vertical)
        #if os(iOS)
            .focused($keyboardFocus)
        #endif
        #if os(visionOS)
        .textFieldStyle(.roundedBorder)
        .hoverEffectDisabled()
        #else
        .textFieldStyle(.plain)
        #endif
        .textInputAutocapitalization(.sentences)
        .lineLimit(3)
        .submitLabel(.send)
        .onSubmit {
            // Will be called on macOS/Simulator with hardware keyboard
            Task {
                await sendMessage()
            }
        }
        .onChange(of: text.last?.isNewline ?? false) { _, forceSubmit in
            // onSubmit won't be called by the submit key when using a software keyboard with a .vertical TextField
            if forceSubmit {
                Task {
                    await sendMessage()
                }
            }
        }
        #if !os(visionOS)
        .foregroundStyle(.fg1)
        #endif
        .padding()
    }

    @ViewBuilder
    private func sendButton() -> some View {
        AsyncButton(action: sendMessage) {
            Image(systemName: "arrow.up")
                .frame(width: 8 * .grid, height: 8 * .grid)
        }
        #if os(iOS)
        .padding([.bottom, .trailing], 3 * .grid)
        #else
        .padding([.bottom, .trailing], 2 * .grid)
        #endif
        .disabled(isSendDisabled)
        #if os(visionOS)
            .buttonStyle(.plain)
        #else
            .buttonStyle(RoundButtonStyle())
        #endif
            .accessibilityLabel("chat.send")
            .accessibilityHint("chat.send.hint")
    }

    /// Stop/interrupt while a reply is generating: sends `lk.chat.interrupt`
    /// (NOT `session.end()`) and returns the composer to the ready state.
    @ViewBuilder
    private func stopButton() -> some View {
        AsyncButton {
            store.stopGenerating()
            await controller.interrupt()
        } label: {
            Image(systemName: "stop.fill")
                .frame(width: 8 * .grid, height: 8 * .grid)
        }
        #if os(iOS)
        .padding([.bottom, .trailing], 3 * .grid)
        #else
        .padding([.bottom, .trailing], 2 * .grid)
        #endif
        #if os(visionOS)
        .buttonStyle(.plain)
        #else
        .buttonStyle(StopButtonStyle())
        #endif
        .accessibilityLabel("chat.stop")
        .accessibilityHint("chat.stop.hint")
    }

    private var isSendDisabled: Bool {
        text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private func sendMessage() async {
        guard !isSendDisabled else { return }
        let message = text
        text = ""
        TraceLog.log("text_sent", ["text": message])
        keyboardFocus = false
        sendFeedback += 1
        await session.send(text: message)
    }
}

/// The stop button (filled circle with a tinted border, distinct from the
/// accent-colored send button).
struct StopButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(MemoryFont.labelEmphasis)
            .foregroundStyle(.fgSerious)
            .background(
                Circle()
                    .fill(.bgSerious.opacity(configuration.isPressed ? 0.7 : 1))
            )
            // Expand the hit target to 44pt without growing the 32pt circle.
            .frame(minWidth: 44, minHeight: 44)
            .contentShape(Rectangle())
    }
}
