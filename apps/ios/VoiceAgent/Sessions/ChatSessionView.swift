import Combine
import Foundation
import LiveKit
import LiveKitComponents
import SwiftUI

/// One chat: the recorded conversation and the live turn stitched into a single
/// transcript, with a Text | Voice switch.
///
/// Text is the gateway's HTTP chat stream (`POST /api/chat`, SSE) — no room, no
/// token, no worker — so it works on a deployment with no LiveKit stack and on a
/// project with voice disabled. Voice mints a token and joins the room for the
/// same conversation; switching back leaves the room and keeps the transcript.
///
/// History comes from `GET /api/session` (``ChatSessionHistoryLoader`` into
/// ``ChatSessionTranscriptStore``), live text turns from ``HTTPChatTransport``,
/// live voice turns from the room (`session.messages` plus
/// ``ChatActivityStore``). All share the same bubbles.
///
/// Both modes can be interrupted. Voice interrupts the room
/// (`lk.chat.interrupt`); text cancels the run through ``HTTPChatTransport``'s
/// `POST /api/chat/runs/{runId}/cancel`, because the gateway runs the agent
/// detached and closing the stream alone would leave it going.
struct ChatSessionView: View {
    let route: ChatRoute

    @Environment(ActiveSessionStore.self) var activeSession
    @EnvironmentObject var controller: MemorySessionController
    @EnvironmentObject private var session: Session
    @EnvironmentObject private var localMedia: LocalMediaModel
    @EnvironmentObject private var projectStore: MemoryProjectStore
    @Environment(ChatActivityStore.self) var activity
    @Environment(\.voiceEnabled) private var voiceEnabled
    @Environment(\.dismiss) private var dismiss

    @State var transcript = ChatSessionTranscriptStore()
    /// Text turns: the HTTP transport the web chat uses (no LiveKit at all).
    @State var transport = HTTPChatTransport()
    /// Messages staged while a turn is running, released when it ends.
    @State var queue = ChatQueueStore()
    /// Recorded-history fetches for this chat. It owns "already loaded" so a
    /// cancelled request cannot mark a conversation as loaded and leave its
    /// failure banner with no retry.
    @State var history = ChatSessionHistoryLoader()
    @State private var messageText = ""
    @FocusState private var keyboardFocus: Bool

    var body: some View {
        VStack(spacing: 0) {
            header
            room
            bottomBar
        }
        .background(.bg1)
        // The ScreenHeader chevron is the only back affordance; hide the system
        // bar so a pushed chat cannot show two.
        #if os(iOS)
            .toolbar(.hidden, for: .navigationBar)
            .navigationBarBackButtonHidden(true)
        #endif
            .task(id: ObjectIdentifier(controller)) { await start() }
            .onChange(of: voiceEnabled) { _, enabled in
                // Voice was turned off for the project while a call was open:
                // fall back to text instead of leaving a dead call surface.
                if !enabled, activeSession.mode == .voice {
                    setMode(.text)
                }
            }
            .onChange(of: transport.interruptedTurn) { _, turn in
                guard turn != nil else { return }
                Task { await recoverInterruptedTurn() }
            }
            .onChange(of: transport.unansweredTurn) { _, turn in
                guard turn != nil else { return }
                Task { await recoverUnansweredTurn() }
            }
            .onReceive(session.objectWillChange) { _ in
                // Only the voice room feeds the transcript from LiveKit; a text
                // session has no room and must keep its HTTP live turns.
                guard activeSession.mode == .voice else { return }
                // `objectWillChange` fires before the value is stored; defer a
                // tick so the fresh message list is read.
                Task { @MainActor in
                    await Task.yield()
                    transcript.syncLive(messages: session.messages)
                }
            }
    }

    // MARK: - Header

    private var header: some View {
        ScreenHeader(
            title: headerTitle,
            subtitle: Text(headerSubtitle),
            subtitleFont: route.conversationID == nil ? MemoryFont.meta : MemoryFont.metaMono,
            // No mode picker when the project has voice disabled: there is no
            // call to switch to, and text must not look conditional.
            trailing: voiceEnabled ? AnyView(modePicker) : nil
        ) {
            dismiss()
        }
    }

    private var headerTitle: Text {
        if let title = route.title, !title.isEmpty {
            return Text(title)
        }
        return Text(route.agentName)
    }

    private var headerSubtitle: String {
        if let conversationID = openConversationID {
            return conversationID
        }
        return String(localized: "chats.new")
    }

    /// Text / Voice switch. Voice joins the conversation's room; text leaves it.
    /// The conversation itself never changes.
    private var modePicker: some View {
        Picker("chat.mode.title", selection: modeBinding) {
            Text("chat.mode.text").tag(ChatMode.text)
            Text("chat.mode.voice").tag(ChatMode.voice)
        }
        .pickerStyle(.segmented)
        .frame(width: 40 * .grid)
        .accessibilityLabel("chat.mode.title")
    }

    private var modeBinding: Binding<ChatMode> {
        Binding(
            get: { activeSession.mode },
            set: { setMode($0) }
        )
    }
}

extension ChatSessionView {
    private func setMode(_ mode: ChatMode) {
        guard activeSession.mode != mode else { return }
        activeSession.mode = mode
        Task {
            switch mode {
            case .voice:
                // Voice is the only mode that needs a room: mint a token and
                // join the conversation's room now.
                await connectVoiceIfNeeded()
                await applyCaptureForMode()
            case .text:
                // Leaving voice releases the room; the conversation (and its
                // transcript) stays open for text. The room was the live source
                // while in voice, so bring this chat's own HTTP turns back and
                // pick up whatever the call persisted.
                await endVoiceSession()
                // Reload first: `syncLive` is what drops live entries the
                // recorded transcript already holds, so syncing before the load
                // would re-render every HTTP turn twice.
                await loadHistoryIfNeeded(force: true)
                transport.refreshLiveTranscript(transcript)
            }
        }
    }

    /// Connects the voice session for the open conversation if it is not
    /// already running.
    ///
    /// A text-first chat created its conversation over HTTP, but the controller's
    /// token request predates that id. Point the session at it and let the app
    /// root rebuild the controller (`install()` reacts to `launchKey`), so voice
    /// joins the conversation the user was chatting in; this view's `.task`
    /// re-runs and ``adoptRoute()`` starts the room.
    private func connectVoiceIfNeeded() async {
        guard controller.phase == .idle else { return }
        if let learned = transport.conversationID, learned != activeSession.launchConversationID {
            activeSession.open(agentName: route.agentName, conversationID: learned)
            // `open` resets to text; this chat is switching to voice.
            activeSession.mode = .voice
            return
        }
        await controller.start()
    }

    /// Ends the room (if any) without touching the conversation or its
    /// transcript.
    private func endVoiceSession() async {
        guard controller.phase != .idle else { return }
        await controller.endSession()
    }

    /// Capture follows the mode, including after a fresh connect: text sessions
    /// must never broadcast room audio, voice sessions must be audible.
    private func applyCaptureForMode() async {
        guard session.isConnected else { return }
        switch activeSession.mode {
        case .voice:
            if !localMedia.isMicrophoneEnabled {
                await localMedia.toggleMicrophone()
            }
        case .text:
            if localMedia.isMicrophoneEnabled {
                await localMedia.toggleMicrophone()
            }
        }
    }

    // MARK: - Room

    private var room: some View {
        ZStack {
            if activeSession.mode == .voice {
                voiceRoom
            } else {
                ChatTranscriptView(
                    transcript: transcript,
                    agentName: route.agentName,
                    showsSuggestions: route.conversationID == nil,
                    onSuggestion: { prompt in
                        messageText = prompt
                        keyboardFocus = true
                    },
                    onHistoryRetry: retryHistory,
                    onHistoryApproval: decideHistoryApproval,
                    onLiveApproval: decideApproval,
                    onQuestion: answerQuestion,
                    onSurfaceAction: submitSurfaceAction
                )
            }
            if activeSession.mode == .voice, case let .failed(failure) = controller.phase {
                MemoryFailureView(failure: failure) {
                    Task { await controller.retry() }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(.bg1)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var voiceRoom: some View {
        ZStack {
            VoiceInteractionView()
            if controller.phase == .connecting {
                LoadingStateView(title: "chat.connecting")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(.bg1)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        // The in-call surface ignores the safe area; clip it so it cannot draw
        // under the header or the controls.
        .clipped()
    }

    // MARK: - Bottom bar

    private var bottomBar: some View {
        VStack(spacing: 0) {
            // Live child/subagent runs stay visible across text and voice modes.
            SubagentTrackView()
            // Hang-up belongs to a call, not to a chat: a text session has no
            // room to leave.
            if activeSession.mode == .voice, session.isConnected {
                ChatSessionControls(mode: activeSession.mode, onHangUp: hangUp)
            }
            if activeSession.mode == .text {
                if let error = transport.errorMessage {
                    Text(error)
                        .font(MemoryFont.meta)
                        .foregroundStyle(.fgSerious)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 4 * .grid)
                        .padding(.bottom, 1 * .grid)
                }
                if !queue.items.isEmpty {
                    ChatQueueRowsView(
                        items: queue.items,
                        isRunning: transport.isStreaming,
                        onEdit: editQueuedText,
                        onSendNext: sendNextQueued,
                        onRemove: removeQueued
                    )
                }
                ChatInputView(
                    keyboardFocus: $keyboardFocus,
                    text: $messageText,
                    onSend: sendText,
                    onEnqueue: enqueueText,
                    // A text turn can be cancelled: the stream's `meta` frame
                    // gives the run id and the transport cancels it. While it
                    // streams, Return and Send stage the next message in the
                    // queue instead of dropping it; the stop button stays.
                    mode: transport.isStreaming ? .running : .idle,
                    onStop: stopTextTurn
                )
            }
        }
        .animation(.default, value: activeSession.mode)
        .animation(.default, value: session.isConnected)
    }

    // MARK: - Lifecycle

    /// Runs when the view appears (or its controller is replaced): adopt the
    /// route, fetch recorded history once, and connect only if the session is in
    /// voice mode. Text needs no connection.
    private func start() async {
        transport.onConversationLearned = { conversationID in
            activeSession.learnConversationID(conversationID)
            queue.learnConversationID(conversationID)
        }
        // A turn ending on its own releases the queue parked behind it. A stop
        // does not: the user ended that turn on purpose.
        let sender = makeQueueSender()
        transport.onTurnFinished = makeQueuePump(sender)
        switch adoptRoute() {
        case .unchanged:
            scopeQueue()
            await loadHistoryIfNeeded()
            maybeReleaseQueueOnReopen()
        case .restartInPlace:
            await endVoiceSession()
            resetTranscript()
            scopeQueue()
            await loadHistoryIfNeeded()
            maybeReleaseQueueOnReopen()
        case .rebuiltController:
            // Adopting the route changed `launchKey`, so the app root is
            // replacing the controller and this `.task` is about to be
            // cancelled and re-run against it. Loading here would only start a
            // request that cancellation kills; the re-run (which takes
            // `.unchanged`) is what fetches.
            return
        }
        if activeSession.mode == .voice {
            await connectVoiceIfNeeded()
        }
        await applyCaptureForMode()
    }

    /// Loads the recorded transcript. A brand-new chat has no id until its
    /// first turn, so the conversation the session is pointed at (or the one
    /// the HTTP transport learned) is used when the route carries none. `force`
    /// reloads even when this id is already loaded — the call in between may
    /// have appended records.
    private func loadHistoryIfNeeded(force: Bool = false) async {
        guard
            let conversationID = route.conversationID
            ?? transport.conversationID
            ?? activeSession.launchConversationID
            ?? activeSession.liveConversationID
        else { return }
        await history.load(conversationID: conversationID, force: force, into: transcript)
    }

    private func retryHistory() {
        Task { await loadHistoryIfNeeded(force: true) }
    }

    private func hangUp() async {
        await controller.endSession()
        dismiss()
    }
}
