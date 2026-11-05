import LiveKitComponents
import SwiftUI

/// A multiplatform view that shows the chat input text field, the send button
/// (which becomes a stop/interrupt button while the agent is replying), and
/// the focused composer text (lifted so suggested-prompt chips can fill it).
///
/// Sending is injectable so the same composer serves the LiveKit voice room
/// (`session.send`) and the HTTP text transport, which has no room at all.
/// `allowsInterrupt` says whether this surface can stop a reply: voice can
/// (`lk.chat.interrupt`), text cannot yet — a device credential has no run
/// cancellation route, and closing the HTTP stream leaves the server-side run
/// going. In that case the composer keeps the send button (disabled while a
/// turn streams) instead of offering a stop that does not stop anything.
struct ChatInputView: View {
    @EnvironmentObject private var session: Session
    @EnvironmentObject private var controller: MemorySessionController
    @Environment(ChatActivityStore.self) private var store

    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    @FocusState.Binding var keyboardFocus: Bool
    @Binding var text: String

    /// Overrides the default (LiveKit room) send behaviour.
    private let onSend: ((String) async -> Void)?
    /// Whether this surface can interrupt the reply in flight.
    private let allowsInterrupt: Bool

    /// Bumped on each send to drive the send haptic.
    @State private var sendFeedback = 0

    init(
        keyboardFocus: FocusState<Bool>.Binding,
        text: Binding<String>,
        onSend: ((String) async -> Void)? = nil,
        allowsInterrupt: Bool = true
    ) {
        _keyboardFocus = keyboardFocus
        _text = text
        self.onSend = onSend
        self.allowsInterrupt = allowsInterrupt
    }

    var body: some View {
        // Center the button on the composer: its 44pt hit target sits around a
        // 32pt circle, so bottom-aligning it (with extra bottom padding) made
        // the circle ride visibly high against a one-line text field.
        HStack(alignment: .center, spacing: 3 * .grid) {
            textField()
            if store.isGeneratingReply, allowsInterrupt {
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
        .padding(.trailing, 3 * .grid)
        #else
        .padding(.trailing, 2 * .grid)
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
        .padding(.trailing, 3 * .grid)
        #else
        .padding(.trailing, 2 * .grid)
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
        // A surface that cannot interrupt must not queue a second turn: the
        // transport runs one at a time and would drop it.
        if !allowsInterrupt, store.isGeneratingReply {
            return true
        }
        return text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private func sendMessage() async {
        guard !isSendDisabled else { return }
        let message = text
        text = ""
        TraceLog.log("text_sent", ["text": message])
        keyboardFocus = false
        sendFeedback += 1
        if let onSend {
            await onSend(message)
        } else {
            await session.send(text: message)
        }
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
