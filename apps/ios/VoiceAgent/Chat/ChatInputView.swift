import LiveKitComponents
import SwiftUI

/// A multiplatform view that shows the chat input text field, the send button
/// (which becomes a stop/interrupt button while the agent is replying), and
/// the focused composer text (lifted so suggested-prompt chips can fill it).
struct ChatInputView: View {
    @EnvironmentObject private var session: Session
    @EnvironmentObject private var controller: MemorySessionController

    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    @FocusState.Binding var keyboardFocus: Bool
    @Binding var text: String

    private var store: ChatActivityStore { controller.chatActivity }

    init(keyboardFocus: FocusState<Bool>.Binding, text: Binding<String>) {
        _keyboardFocus = keyboardFocus
        _text = text
    }

    var body: some View {
        HStack(alignment: .bottom, spacing: 12) {
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
            .background(.bg2)
            .overlay(
                RoundedRectangle(cornerRadius: 6 * .grid)
                    .stroke(.separator1.opacity(0.4), lineWidth: 1)
            )
        #endif
            .clipShape(RoundedRectangle(cornerRadius: 6 * .grid))
            .safeAreaPadding(.horizontal, 4 * .grid)
            .safeAreaPadding(.bottom, 4 * .grid)
    }

    @ViewBuilder
    private func textField() -> some View {
        TextField("Message", text: $text, axis: .vertical)
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
        await session.send(text: message)
    }
}

/// The stop button (filled circle with a tinted border, distinct from the
/// accent-colored send button).
struct StopButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 12, weight: .semibold))
            .foregroundStyle(.fgSerious)
            .background(
                Circle()
                    .fill(.bgSerious.opacity(configuration.isPressed ? 0.7 : 1))
            )
    }
}
