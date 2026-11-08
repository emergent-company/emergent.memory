import LiveKitComponents
import SwiftUI

/// What Return and the send button mean right now.
///
/// `idle`: a send starts a turn. `running`: a turn is in flight, so a send
/// stages the text in the composer queue and the stop button is live.
enum ChatComposerMode: Equatable {
    case idle
    case running
}

/// A multiplatform view that shows the chat input text field, the send button
/// (which becomes a stop/interrupt button while the agent is replying), and
/// the focused composer text (lifted so suggested-prompt chips can fill it).
///
/// Sending is injectable so the same composer serves the LiveKit voice room
/// (`session.send`) and the HTTP text transport, which has no room at all.
/// `onStop` says whether this surface can stop a reply: voice leaves it nil and
/// the room is interrupted, text passes the transport's run cancel.
/// `mode` says whether a turn is running, which decides what Return and Send do
/// — while running they stage the message in the queue instead of dropping it.
struct ChatInputView: View {
    @EnvironmentObject private var session: Session
    @EnvironmentObject private var controller: MemorySessionController
    @Environment(ChatActivityStore.self) private var store

    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    @FocusState.Binding var keyboardFocus: Bool
    @Binding var text: String

    /// Overrides the default (LiveKit room) send behaviour.
    private let onSend: ((String) async -> Void)?
    /// Stages the text in the queue instead of sending it. Text passes this; a
    /// voice composer leaves it nil and ⌘↩ falls back to a normal send.
    private let onEnqueue: ((String) -> Void)?
    /// What a stop does. Voice leaves it nil and the room is interrupted; text
    /// passes the transport's run cancel.
    private let onStop: (() async -> Void)?
    /// Whether a turn is in flight, which decides what Return/Send do.
    private let mode: ChatComposerMode

    /// Bumped on each send to drive the send haptic.
    @State private var sendFeedback = 0
    /// Set when ⇧↩ asks the text field for a newline, so the newline-detection
    /// submit path below does not treat that newline as a send.
    @State private var insertingNewline = false

    init(
        keyboardFocus: FocusState<Bool>.Binding,
        text: Binding<String>,
        onSend: ((String) async -> Void)? = nil,
        onEnqueue: ((String) -> Void)? = nil,
        mode: ChatComposerMode = .idle,
        onStop: (() async -> Void)? = nil
    ) {
        _keyboardFocus = keyboardFocus
        _text = text
        self.onSend = onSend
        self.onEnqueue = onEnqueue
        self.mode = mode
        self.onStop = onStop
    }

    var body: some View {
        // Center the button on the composer: its 44pt hit target sits around a
        // 32pt circle, so bottom-aligning it (with extra bottom padding) made
        // the circle ride visibly high against a one-line text field.
        HStack(alignment: .center, spacing: 3 * .grid) {
            textField()
            // While a turn runs both buttons are live: the stop interrupts,
            // the send stages the follow-up instead of dropping it.
            if showsStopButton {
                stopButton()
            }
            if mode == .running || !showsStopButton {
                sendButton()
            }
            queueShortcut
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
        // A hardware keyboard's Return follows the turn state: send when idle,
        // stage while running; ⇧↩ asks the field for a newline. A ⌘↩ is a key
        // equivalent handled by `queueShortcut`, so it is left alone here.
        .onKeyPress(.return, phases: .down) { press in
            if press.modifiers.contains(.command) {
                return .ignored
            }
            if press.modifiers.contains(.shift) {
                insertingNewline = true
                return .ignored
            }
            Task {
                await sendMessage()
            }
            return .handled
        }
        .onChange(of: text.last?.isNewline ?? false) { _, forceSubmit in
            // onSubmit won't be called by the submit key when using a software keyboard with a .vertical TextField
            guard forceSubmit else { return }
            if insertingNewline {
                insertingNewline = false
                return
            }
            Task {
                await sendMessage()
            }
        }
        #if !os(visionOS)
        .foregroundStyle(.fg1)
        #endif
        .padding()
    }

    /// ⌘↩ always stages the message, even when the composer is idle: the
    /// unambiguous way to park a follow-up without changing what Return means.
    /// A hidden button carries the key equivalent, the way SwiftUI exposes one.
    private var queueShortcut: some View {
        Button("chat.queue.shortcut") {
            enqueueMessage()
        }
        .keyboardShortcut(.return, modifiers: .command)
        .opacity(0)
        .frame(width: 0, height: 0)
        .accessibilityHidden(true)
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

    /// Stop/interrupt while a reply is in flight. The activity store is settled
    /// first so the working line and spinners stop immediately; then `onStop`
    /// cancels the run (text) or `lk.chat.interrupt` is sent (voice, NOT
    /// `session.end()`).
    @ViewBuilder
    private func stopButton() -> some View {
        AsyncButton {
            store.stopGenerating()
            if let onStop {
                await onStop()
            } else {
                await controller.interrupt()
            }
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

    /// Text mode takes the transport's flag; a voice surface has no transport,
    /// so the activity store's reply window is its stop signal.
    private var showsStopButton: Bool {
        if mode == .running {
            return true
        }
        return onStop == nil && store.isGeneratingReply
    }

    private var isSendDisabled: Bool {
        text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    /// Send follows the turn state: a running turn stages the text, an idle one
    /// starts a turn. Staging keeps the keyboard up for the next follow-up.
    private func sendMessage() async {
        guard !isSendDisabled else { return }
        let message = text
        text = ""
        sendFeedback += 1
        if mode == .running {
            TraceLog.log("text_queued", ["text": message])
            await enqueue(message)
            return
        }
        keyboardFocus = false
        TraceLog.log("text_sent", ["text": message])
        if let onSend {
            await onSend(message)
        } else {
            await session.send(text: message)
        }
    }

    /// ⌘↩: always stage, never send — a queued follow-up the user releases
    /// explicitly (or that waits for the next running turn to arm it).
    private func enqueueMessage() {
        guard !isSendDisabled else { return }
        let message = text
        text = ""
        sendFeedback += 1
        guard let onEnqueue else {
            // A surface with no queue (voice): ⌘↩ is still a send.
            Task { await sendMessage(message) }
            return
        }
        TraceLog.log("text_queued", ["text": message])
        onEnqueue(message)
    }

    private func enqueue(_ message: String) async {
        if let onEnqueue {
            onEnqueue(message)
            return
        }
        if let onSend {
            await onSend(message)
        } else {
            await session.send(text: message)
        }
    }

    /// Sends a message that was already taken out of the field.
    private func sendMessage(_ message: String) async {
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
