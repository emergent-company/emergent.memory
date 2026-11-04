import Combine
import Foundation
import LiveKit
import LiveKitComponents
import SwiftUI

/// One chat: the recorded conversation and the live turn stitched into a single
/// transcript, with a Text | Voice switch.
///
/// Text is served over the gateway's HTTP chat stream (`POST /api/chat`, SSE) —
/// no room, no token, no worker — so it works on a deployment with no LiveKit
/// stack and on a project with voice disabled. Voice mints a token and joins the
/// LiveKit room for the same conversation; switching back leaves the room and
/// keeps the transcript.
///
/// History comes from `GET /api/session` (``ChatSessionTranscriptStore``), live
/// text turns from ``HTTPChatTransport``, and live voice turns from the room
/// (`session.messages` plus the rich ``ChatActivityStore``). All of them share
/// the same bubbles, tool cards and approval cards.
///
/// Interrupting is a room feature: text mode has no stop control because a
/// device credential cannot cancel a run yet (`emergent.memory#81`) and closing
/// the stream would leave the server-side run going.
struct ChatSessionView: View {
    let route: ChatRoute

    @Environment(ActiveSessionStore.self) private var activeSession
    @EnvironmentObject private var controller: MemorySessionController
    @EnvironmentObject private var session: Session
    @EnvironmentObject private var localMedia: LocalMedia
    @Environment(ChatActivityStore.self) private var activity
    @Environment(\.voiceEnabled) private var voiceEnabled
    @Environment(\.dismiss) private var dismiss

    @State private var transcript = ChatSessionTranscriptStore()
    /// Text turns: the HTTP transport the web chat uses (no LiveKit at all).
    @State private var transport = HTTPChatTransport()
    @State private var messageText = ""
    /// The conversation id whose history is loaded, so a re-render (or a
    /// controller rebuild for the same room) never fetches twice.
    @State private var loadedConversationID: String?
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
                transport.refreshLiveTranscript(transcript)
                await loadHistoryIfNeeded(force: true)
            }
        }
    }

    /// Connects the voice session for the open conversation if it is not
    /// already running.
    ///
    /// A text-first chat created its conversation over HTTP, and the
    /// controller's token request was built before that id existed. Point the
    /// session at it and let the app root rebuild the controller (`install()`
    /// reacts to `launchKey`), so voice joins the conversation the user was
    /// already chatting in instead of minting a second, empty one. The view's
    /// `.task` re-runs for the new controller and takes the adopted-conversation
    /// branch in ``adoptRoute()``, which starts the room.
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

    // MARK: - Decisions

    private func decideApproval(_ request: ApprovalRequest, _ decision: ApprovalDecision) {
        guard !request.isAnswered else { return }
        activity.submitApproval(questionId: request.questionId, decision: decision)
        if activeSession.mode == .text {
            let response: String
            let message: String
            switch decision {
            case .approve:
                response = "approve"
                message = ""
            case let .reject(reason):
                response = "reject"
                message = reason ?? ""
            }
            respondOverHTTP(questionId: request.questionId, response: response, message: message)
            return
        }
        Task { await controller.sendDecision(chatDecision(for: decision, questionId: request.questionId)) }
    }

    /// The open conversation: the route's, the id the HTTP `meta` frame taught
    /// us, or the worker's reported id.
    private var openConversationID: String? {
        route.conversationID ?? transport.conversationID ?? activeSession.liveConversationID
    }

    /// Answers a live card over HTTP (text mode): the gateway resumes the paused
    /// run in the background, so the transcript is polled for the continuation.
    private func respondOverHTTP(questionId: String, response: String, message: String) {
        guard let conversationID = openConversationID else { return }
        let baseline = transcript.lastRecordCount
        Task {
            do {
                try await transport.respond(questionID: questionId, response: response, message: message)
                await pollResumedHistory(
                    client: SessionLogClient(config: MemoryConfig()),
                    conversationID: conversationID,
                    baseline: baseline
                )
            } catch {
                transcript.historyLoadFailed(error.localizedDescription)
            }
        }
    }

    /// Recorded approvals belong to a run that is not attached to this room's
    /// worker, so they are answered through the gateway's question endpoint.
    /// Memory resumes the run in the background; the transcript is polled for
    /// the continuation because it does not stream back over LiveKit.
    private func decideHistoryApproval(_ request: ApprovalRequest, _ decision: ApprovalDecision) {
        guard let conversationID = openConversationID else {
            return
        }
        let client = SessionLogClient(config: MemoryConfig())
        let baseline = transcript.lastRecordCount
        transcript.markApprovalAnswered(questionId: request.questionId)
        Task {
            do {
                let response: String
                let message: String
                switch decision {
                case .approve:
                    response = "approve"
                    message = ""
                case let .reject(reason):
                    response = "reject"
                    message = reason ?? ""
                }
                try await client.respondQuestion(
                    questionID: request.questionId,
                    response: response,
                    message: message
                )
                await pollResumedHistory(client: client, conversationID: conversationID, baseline: baseline)
            } catch {
                transcript.historyLoadFailed(error.localizedDescription)
            }
        }
    }

    /// Waits for the resumed run to append records, then reloads the history.
    private func pollResumedHistory(
        client: SessionLogClient,
        conversationID: String,
        baseline: Int
    ) async {
        for _ in 0 ..< 30 {
            try? await Task.sleep(for: .seconds(1))
            guard let detail = try? await client.detail(room: conversationID) else { continue }
            if detail.records.count > baseline {
                transcript.loadHistory(from: detail)
                return
            }
        }
    }

    private func answerQuestion(_ request: QuestionRequest, answer: String) {
        guard !request.isAnswered else { return }
        activity.submitQuestion(questionId: request.questionId, answer: answer)
        if activeSession.mode == .text {
            respondOverHTTP(questionId: request.questionId, response: answer, message: "")
            return
        }
        Task { await controller.sendDecision(.answer(questionId: request.questionId, value: answer)) }
    }

    private func submitSurfaceAction(_ surface: ChatUISurface, _ action: ChatUIAction) {
        // A generic A2UI surface action has no HTTP transport yet (product #66),
        // so a text session renders surfaces read-only — the same thing the web
        // chat can do today. Voice keeps the worker round-trip. Approval- and
        // question-shaped cards are answered through the routes above.
        guard activeSession.mode == .voice else { return }
        activity.submitSurfaceAction(
            surfaceId: surface.id,
            componentId: action.componentId,
            response: action.response
        )
        Task { await controller.sendDecision(.surfaceAction(surfaceId: surface.id, action: action)) }
    }

    private func chatDecision(for decision: ApprovalDecision, questionId: String) -> ChatDecision {
        switch decision {
        case .approve:
            .approve(questionId: questionId)
        case let .reject(reason):
            .reject(questionId: questionId, reason: reason)
        }
    }

    // MARK: - Bottom bar

    private var bottomBar: some View {
        VStack(spacing: 0) {
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
                ChatInputView(
                    keyboardFocus: $keyboardFocus,
                    text: $messageText,
                    onSend: sendText,
                    // No run cancellation from a device credential yet, and
                    // closing the stream leaves the server-side run going, so
                    // text offers no stop control rather than a fake one.
                    allowsInterrupt: false
                )
            }
        }
        .animation(.default, value: activeSession.mode)
        .animation(.default, value: session.isConnected)
    }

    // MARK: - Text turns

    /// Sends a text turn over the HTTP chat stream. Never mints a token and
    /// never joins a room.
    private func sendText(_ text: String) {
        transport.send(
            text,
            conversationID: openConversationID,
            agentName: route.agentName,
            transcript: transcript,
            activity: activity
        )
    }

    // MARK: - Lifecycle

    /// What the route asked for, versus what the active session already holds.
    private enum RouteAdoption {
        /// The controller is already listening on the route's conversation.
        case unchanged
        /// A new controller is being installed by the app root.
        case rebuiltController
        /// The same room configuration was restarted as a fresh conversation.
        case restartInPlace
    }

    /// Runs when the view appears (or its controller is replaced): adopt the
    /// route, fetch recorded history once, and connect only if the session is in
    /// voice mode. Text needs no connection.
    private func start() async {
        transport.onConversationLearned = { conversationID in
            activeSession.learnConversationID(conversationID)
        }
        switch adoptRoute() {
        case .unchanged:
            await loadHistoryIfNeeded()
        case .restartInPlace:
            await endVoiceSession()
            resetTranscript()
            await loadHistoryIfNeeded()
        case .rebuiltController:
            // The root rebuilds the controller for the new launch key; this
            // view's task re-runs with that controller and takes `.unchanged`.
            await loadHistoryIfNeeded()
        }
        if activeSession.mode == .voice {
            await connectVoiceIfNeeded()
        }
        await applyCaptureForMode()
    }

    private func adoptRoute() -> RouteAdoption {
        let sameAgent = activeSession.agentName == route.agentName
        let sameLaunch = activeSession.launchConversationID == route.conversationID

        guard let conversationID = route.conversationID else {
            // This "new chat" may have created its conversation over HTTP and
            // then been re-pointed at it so voice could join. The transport
            // still holds that id, so adopt the installed controller instead of
            // starting a second conversation.
            if sameAgent, activeSession.launchConversationID != nil,
               activeSession.launchConversationID == transport.conversationID
            {
                return .unchanged
            }
            if sameAgent, sameLaunch {
                // Re-entering "new chat" after a live id was learned: the same
                // launch key is still valid, so start a fresh conversation in
                // place rather than rebuilding the controller.
                guard activeSession.liveConversationID != nil else { return .unchanged }
                activeSession.startNew(agentName: route.agentName, startsMuted: true)
                return .restartInPlace
            }
            activeSession.startNew(agentName: route.agentName, startsMuted: true)
            resetTranscript()
            return .rebuiltController
        }

        if sameAgent, sameLaunch {
            return .unchanged
        }
        activeSession.open(
            agentName: route.agentName,
            conversationID: conversationID,
            startsMuted: true
        )
        resetTranscript()
        return .rebuiltController
    }

    private func resetTranscript() {
        transcript.reset()
        transport.reset()
        loadedConversationID = nil
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
        if !force, loadedConversationID == conversationID {
            return
        }
        loadedConversationID = conversationID
        transcript.beginHistoryLoad()
        do {
            let detail = try await SessionLogClient(config: MemoryConfig()).detail(room: conversationID)
            // A new route may have reset the transcript while this fetch was in
            // flight; never apply a stale conversation's history.
            guard loadedConversationID == conversationID else { return }
            transcript.loadHistory(from: detail)
        } catch {
            guard loadedConversationID == conversationID else { return }
            transcript.historyLoadFailed(error.localizedDescription)
        }
    }

    private func retryHistory() {
        loadedConversationID = nil
        Task { await loadHistoryIfNeeded() }
    }

    private func hangUp() async {
        await controller.endSession()
        dismiss()
    }
}
