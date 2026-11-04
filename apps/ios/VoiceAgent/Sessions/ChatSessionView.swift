import Combine
import Foundation
import LiveKit
import LiveKitComponents
import SwiftUI

/// One chat room: the recorded conversation and the live session stitched into
/// a single transcript, with a Text | Voice switch over the same LiveKit room.
///
/// History comes from `GET /api/session` (``ChatSessionTranscriptStore``) and
/// live turns stream in from the room (`session.messages` plus the rich
/// ``ChatActivityStore``). Both share the same bubbles, tool cards and approval
/// cards, so reopening a conversation reads like it did live.
///
/// Switching mode never reconnects or changes the conversation: text enables
/// the composer, voice shows the same in-call surface as the Conversation tab
/// and turns the microphone on.
struct ChatSessionView: View {
    let route: ChatRoute

    @Environment(ActiveSessionStore.self) private var activeSession
    @EnvironmentObject private var controller: MemorySessionController
    @EnvironmentObject private var session: Session
    @EnvironmentObject private var localMedia: LocalMedia
    @Environment(ChatActivityStore.self) private var activity
    @Environment(\.dismiss) private var dismiss

    @State private var transcript = ChatSessionTranscriptStore()
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
            .onReceive(session.objectWillChange) { _ in
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
            trailing: AnyView(modePicker)
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
        if let conversationID = route.conversationID {
            return conversationID
        }
        return String(localized: "chats.new")
    }

    /// Text / Voice switch for the room. Changing it only flips local capture
    /// state — the room, conversation and controller stay exactly as they are.
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
        Task { await applyCaptureForMode() }
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
            if case let .failed(failure) = controller.phase {
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
        Task { await controller.sendDecision(chatDecision(for: decision, questionId: request.questionId)) }
    }

    /// Recorded approvals belong to a run that is not attached to this room's
    /// worker, so they are answered through the gateway's question endpoint.
    /// Memory resumes the run in the background; the transcript is polled for
    /// the continuation because it does not stream back over LiveKit.
    private func decideHistoryApproval(_ request: ApprovalRequest, _ decision: ApprovalDecision) {
        guard let conversationID = route.conversationID ?? activeSession.liveConversationID else {
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
        Task { await controller.sendDecision(.answer(questionId: request.questionId, value: answer)) }
    }

    private func submitSurfaceAction(_ surface: ChatUISurface, _ action: ChatUIAction) {
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
            if session.isConnected {
                ChatSessionControls(mode: activeSession.mode, onHangUp: hangUp)
            }
            if activeSession.mode == .text {
                if session.isConnected {
                    ChatInputView(keyboardFocus: $keyboardFocus, text: $messageText)
                } else if controller.phase == .connecting {
                    LoadingStateView(title: "chat.connecting", fillsHeight: false)
                        .padding(.bottom, 4 * .grid)
                }
            }
        }
        .animation(.default, value: activeSession.mode)
        .animation(.default, value: session.isConnected)
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

    /// Runs when the room appears (or its controller is replaced): adopt the
    /// route, fetch recorded history once, then connect.
    private func start() async {
        switch adoptRoute() {
        case .unchanged:
            await loadHistoryIfNeeded()
            if controller.phase == .idle {
                await controller.start()
            }
        case .restartInPlace:
            if controller.phase != .idle {
                await controller.endSession()
            }
            resetTranscript()
            await controller.start()
        case .rebuiltController:
            // The root rebuilds the controller for the new launch key; that
            // controller's own task does the history load and connect.
            break
        }
        await applyCaptureForMode()
    }

    private func adoptRoute() -> RouteAdoption {
        let sameAgent = activeSession.agentName == route.agentName
        let sameLaunch = activeSession.launchConversationID == route.conversationID

        guard let conversationID = route.conversationID else {
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
        loadedConversationID = nil
    }

    private func loadHistoryIfNeeded() async {
        guard let conversationID = route.conversationID else { return }
        guard loadedConversationID != conversationID else { return }
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
