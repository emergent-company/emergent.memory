import AudioToolbox
import AVFoundation
import Combine
import Foundation
import LiveKit

/// User-facing classification of a failed connection (OpenSpec task 3.4).
/// Plain enum + `LocalizedError` so the failure UI stays trivially restyleable.
enum MemoryFailure: LocalizedError, Equatable {
    /// The token endpoint could not be reached, returned a non-2xx status,
    /// or returned an undecodable body.
    case tokenFetch(message: String)
    /// The token was obtained but the LiveKit server did not accept the
    /// connection (unreachable host, bad token, rejected room, ...).
    case serverUnreachable(message: String)
    /// The room connected but the agent never joined within
    /// `MemoryConfig.agentConnectTimeout`.
    case agentNotJoined(agentName: String)
    /// The microphone permission was denied (LiveKit `deviceAccessDenied`).
    case micPermissionDenied(agentName: String)
    /// Any other failure.
    case unknown(message: String)

    var errorDescription: String? {
        switch self {
        case let .tokenFetch(message):
            "Could not get a token from the Memory token server.\n\(message)"
        case let .serverUnreachable(message):
            "Could not connect to the LiveKit server.\n\(message)"
        case let .agentNotJoined(agentName):
            "\(agentName) did not join the room in time. The agent worker may be unavailable."
        case let .micPermissionDenied(agentName):
            "Microphone access is required to talk to \(agentName). Enable it in Settings > Privacy > Microphone."
        case let .unknown(message):
            message
        }
    }
}

/// Drives an Memory voice session on the LiveKit Swift SDK's
/// `Session`/`LocalMedia` architecture: connects + dispatches
/// `memory-google-rt` (agent name flows through the token request), classifies
/// connection failures with a retry affordance, and consumes Memory's
/// text-stream signals (`lk.agent.ready` chime, `lk.agent.events` away,
/// `lk.transcription` exit keywords, agent-left end-of-session).
@MainActor
final class MemorySessionController: ObservableObject {
    /// Connection lifecycle exposed to the UI.
    enum Phase: Equatable {
        case idle
        case connecting
        case connected
        case failed(MemoryFailure)
    }

    /// Memory's text-stream topics (see the worker protocol in `agent/main.py`).
    static let agentReadyTopic = "lk.agent.ready"
    static let agentEventsTopic = "lk.agent.events"
    /// Rich chat activity (worker → client): one JSON `ChatEvent` per message.
    static let chatEventsTopic = "lk.chat.events"
    /// Decisions on approval/question cards (client → worker), JSON per message.
    static let chatDecisionTopic = "lk.chat.decision"
    /// Interrupt the current generation (client → worker): empty text payload.
    static let chatInterruptTopic = "lk.chat.interrupt"

    let session: Session
    let localMedia: LocalMedia

    /// Live rich-chat activity (tool chips, thinking, approval/question cards)
    /// decoded from `lk.chat.events` and rendered by `ChatView`.
    let chatActivity = ChatActivityStore()

    @Published private(set) var phase: Phase = .idle

    private let config: MemoryConfig
    private var cancellables = Set<AnyCancellable>()
    private var signalHandlersRegistered = false
    private var seenTranscriptIDs = Set<String>()
    private var lastMessageCount = 0
    /// Number of `session.messages` already traced as `message_received`
    /// (count-based diff so restored history is never re-traced).
    private var lastTracedMessageCount = 0
    /// Guards the one-shot `agent_joined` trace per session.
    private var agentJoinedTraced = false

    /// Pending coalesced work scheduled from `objectWillChange`: a burst of
    /// changes replaces the previous pending scan/trace instead of spawning a
    /// Task per change.
    private var transcriptScanTask: Task<Void, Never>?
    private var messageTraceTask: Task<Void, Never>?

    /// Speaks agent replies locally (AVSpeechSynthesizer — no server TTS).
    private let speechSynthesizer = AVSpeechSynthesizer()

    init(config: MemoryConfig) {
        self.config = config
        let session = Self.makeSession(config: config)
        self.session = session
        localMedia = LocalMedia(session: session)
        observe()
    }

    /// Builds the `Session` with echo cancellation on from the first captured
    /// frame (Memory uses Gemini-native activity detection — no server VAD —
    /// so barge-in depends on client-side AEC).
    private static func makeSession(config: MemoryConfig) -> Session {
        let tokenSource = AgentToConnect.memory(config: config).tokenSource
        let room = Room(roomOptions: RoomOptions(
            defaultAudioCaptureOptions: AudioCaptureOptions(
                echoCancellation: true,
                echoCancellationMode: .automatic
            )
        ))
        return Session(
            tokenSource: tokenSource,
            tokenOptions: TokenRequestOptions(
                participantIdentity: config.participantIdentity,
                agentName: config.agentName
            ),
            options: SessionOptions(
                room: room,
                preConnectAudio: true,
                agentConnectTimeout: config.agentConnectTimeout
            )
        )
    }

    /// Display name for the configured agent, used in failure messages.
    /// Agent names come from the control plane now (no curated aliases), so
    /// the configured name is the display name.
    private var agentDisplayName: String {
        config.agentName
    }

    /// The agent participant's identity string (empty until it joins the room).
    private var agentIdentity: String {
        session.room.remoteParticipants.values.first?.identity?.stringValue ?? ""
    }

    // MARK: - Lifecycle

    /// Connects to the room and dispatches the configured agent.
    func start() async {
        guard phase == .idle else { return }
        TraceLog.log("phase_changed", ["phase": "connecting"], room: session.room.name ?? "")
        Log.session.info("start connect agent=\(self.config.agentName) endpoint=\(self.config.tokenEndpoint)")
        registerSignalHandlers()
        phase = .connecting
        seenTranscriptIDs.removeAll()
        lastMessageCount = 0
        agentJoinedTraced = false
        // Fresh session: drop any messages retained from a previous connect.
        session.restoreMessageHistory([])
        lastTracedMessageCount = 0
        chatActivity.reset()

        await session.start()

        if let error = session.error {
            let failure = classify(error)
            phase = .failed(failure)
            TraceLog.log("failure", ["case": String(describing: failure), "message": failure.errorDescription ?? ""], room: session.room.name ?? "")
            Log.session.error("connect failed: \(failure.localizedDescription)")
        } else if let agentError = session.agent.error {
            let failure = classify(agentError: agentError)
            phase = .failed(failure)
            TraceLog.log("failure", ["case": String(describing: failure), "message": failure.errorDescription ?? ""], room: session.room.name ?? "")
            Log.session.error("connect failed: \(failure.localizedDescription)")
        } else {
            phase = .connected
            TraceLog.log("livekit_connected", ["room": session.room.name ?? ""], room: session.room.name ?? "")
            Log.session.info("connect ok agent=\(self.config.agentName)")
        }
    }

    /// Ends the session and returns to the idle state.
    func endSession() async {
        guard phase != .idle else { return }
        TraceLog.log("session_ended", room: session.room.name ?? "")
        scanTranscripts() // final sweep before leaving the room
        speechSynthesizer.stopSpeaking(at: .immediate)
        phase = .idle
        seenTranscriptIDs.removeAll()
        lastMessageCount = 0
        chatActivity.reset()
        await session.end()
    }

    /// Ends the failed session and tries again.
    func retry() async {
        TraceLog.log("retry_tapped", room: session.room.name ?? "")
        await endSession()
        await start()
    }

    // MARK: - Observation

    private func observe() {
        session.$agent
            .dropFirst()
            .sink { [weak self] agent in
                self?.handleAgentChange(agent)
                // One-shot trace when the agent first connects.
                if let self, agent.error == nil, agent.isConnected, !agentJoinedTraced {
                    agentJoinedTraced = true
                    TraceLog.log("agent_joined", ["identity": agentIdentity], room: self.session.room.name ?? "")
                }
            }
            .store(in: &cancellables)

        session.objectWillChange
            .sink { [weak self] _ in
                self?.scheduleTranscriptScan()
                self?.scheduleMessageTrace()
                self?.resetPhaseIfDisconnected()
            }
            .store(in: &cancellables)
    }

    /// Returns to idle when the room disconnects (e.g. the user hangs up via
    /// the control bar) so "Connect" works again. The agent often closes with
    /// error == nil rather than `.left`, so this can't rely on
    /// `handleAgentChange` alone.
    private func resetPhaseIfDisconnected() {
        if !session.isConnected, phase == .connected {
            phase = .idle
        }
    }

    private func handleAgentChange(_ agent: LiveKit.Agent) {
        if case .timeout? = agent.error, phase == .connecting || phase == .connected {
            // Room connected but the agent never joined (dispatch failure or
            // worker unavailable). The user can retry from the failure UI.
            phase = .failed(.agentNotJoined(agentName: agentDisplayName))
            TraceLog.log("agent_left", ["identity": agentIdentity, "reason": "timeout"], room: session.room.name ?? "")
        } else if case .left? = agent.error {
            // 4.4: the agent participant left the room — end the session and
            // return the UI to idle.
            TraceLog.log("agent_left", ["identity": agentIdentity, "reason": "left"], room: session.room.name ?? "")
            Task { await endSession() }
        }
    }

    /// Scans `session.messages` (transcription history) and ends the session
    /// when a final user transcript contains an exit keyword.
    ///
    /// `objectWillChange` fires before the value is stored, so the scan is
    /// deferred with `Task.yield()` to read the fresh value on the main actor.
    private func scheduleTranscriptScan() {
        transcriptScanTask?.cancel()
        transcriptScanTask = Task { @MainActor [weak self] in
            await Task.yield()
            guard !Task.isCancelled, let self else { return }
            let count = self.session.messages.count
            guard count != self.lastMessageCount else { return }
            self.lastMessageCount = count
            self.scanTranscripts()
        }
    }

    /// Traces newly arrived `session.messages`. Mirrors `scheduleTranscriptScan`:
    /// `objectWillChange` fires before the value is stored, so read the fresh
    /// value on the main actor after `Task.yield()`.
    private func scheduleMessageTrace() {
        messageTraceTask?.cancel()
        messageTraceTask = Task { @MainActor [weak self] in
            await Task.yield()
            guard !Task.isCancelled, let self else { return }
            // Trace newly arrived messages (role from ReceivedMessage.Content,
            // text truncated to ~200 chars).
            let messages = self.session.messages
            let count = messages.count
            if count > self.lastTracedMessageCount {
                for message in messages.suffix(count - self.lastTracedMessageCount) {
                    let role: String
                    let text: String
                    switch message.content {
                    case let .userInput(content): role = "userInput"; text = content
                    case let .userTranscript(content): role = "userTranscript"; text = content
                    case let .agentTranscript(content): role = "agentTranscript"; text = content
                    }
                    TraceLog.log("message_received", ["role": role, "text": String(text.prefix(200))], room: self.session.room.name ?? "")
                    // Drive the live chat-activity turns from message traffic:
                    // user messages open a turn (anchored to the message), the
                    // first agent transcript of the turn dismisses the typing
                    // indicator and arms the stop-quiet timer.
                    switch message.content {
                    case .userInput(_), .userTranscript(_):
                        if message.isFinal {
                            self.chatActivity.userTurnStarted(anchorMessageID: message.id)
                        }
                    case .agentTranscript:
                        self.chatActivity.agentReplyStarted()
                    }
                    // Speak agent replies locally (free, client-side TTS) only
                    // when the worker is text-only (`ttsStrategy == "client"`);
                    // a `"server"` worker streams its own TTS audio.
                    if case let .agentTranscript(agentText) = message.content, !agentText.isEmpty, self.config.ttsStrategy == "client" {
                        let utterance = AVSpeechUtterance(string: agentText)
                        utterance.voice = AVSpeechSynthesisVoice(language: "en-US")
                        speechSynthesizer.speak(utterance)
                    }
                }
                self.lastTracedMessageCount = count
            }
        }
    }

    private func scanTranscripts() {
        let keywords = config.exitKeywords.map { $0.lowercased() }
        guard !keywords.isEmpty else { return }

        for message in session.messages {
            guard seenTranscriptIDs.insert(message.id).inserted else { continue }
            guard message.isFinal, case let .userTranscript(text) = message.content else { continue }
            let transcript = text.lowercased()
            if keywords.contains(where: { transcript.contains($0) }) {
                Task { await endSession() }
                return
            }
        }
    }

    // MARK: - Memory text-stream signals

    private func registerSignalHandlers() {
        guard !signalHandlersRegistered else { return }
        signalHandlersRegistered = true

        let room = session.room

        // 4.1: cue-to-speak chime on `lk.agent.ready`.
        Task {
            do {
                try await room.registerTextStreamHandler(for: Self.agentReadyTopic) { [weak self] reader, _ in
                    _ = try await reader.readAll()
                    await self?.handleAgentReady()
                }
            } catch {
                // Best-effort: a failed registration only disables the chime.
            }
        }

        // 4.2: `lk.agent.events` — `user_state_changed` to `away` ends the session.
        Task {
            do {
                try await room.registerTextStreamHandler(for: Self.agentEventsTopic) { [weak self] reader, _ in
                    let body = try await reader.readAll()
                    await self?.handleAgentEvent(body)
                }
            } catch {
                // Best-effort: a failed registration only disables away-detection.
            }
        }

        // iOS chat parity: `lk.chat.events` — rich activity (tool calls,
        // thinking, approvals, questions) rendered by the live chat. Older
        // workers never emit on this topic, so this registration is additive.
        Task {
            do {
                try await room.registerTextStreamHandler(for: Self.chatEventsTopic) { [weak self] reader, _ in
                    let body = try await reader.readAll()
                    await self?.handleChatEvents(body)
                }
            } catch {
                // Best-effort: a failed registration only disables rich activity.
            }
        }
    }

    private func handleAgentReady() {
        // `1057` ("Tink") is a short, soft system sound — no bundled asset.
        AudioServicesPlaySystemSound(SystemSoundID(config.chimeSoundID))
    }

    private struct AgentEvent: Decodable {
        let type: String
        let newState: String?

        enum CodingKeys: String, CodingKey {
            case type
            case newState = "new_state"
        }
    }

    private func handleAgentEvent(_ body: String) {
        guard
            let data = body.data(using: .utf8),
            let event = try? JSONDecoder().decode(AgentEvent.self, from: data)
        else { return }

        if event.type == "user_state_changed", event.newState == "away" {
            Task { await endSession() }
        }
    }

    /// Splits the stream body into per-message JSON lines and routes each
    /// decodable `ChatEvent` into the activity store. Unknown `type` values
    /// decode to `nil` and are ignored.
    private func handleChatEvents(_ body: String) {
        let lines = body
            .split(whereSeparator: \.isNewline)
            .map(String.init)
            .filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
        for line in lines {
            guard let event = decodeChatEvent(line) else { continue }
            chatActivity.apply(event)
        }
    }

    // MARK: - Rich-chat decisions (iOS chat parity)

    /// Sends a card decision to the worker over `lk.chat.decision`.
    func sendDecision(_ decision: ChatDecision) async {
        let payload: String
        do {
            payload = try encodeChatDecision(decision)
        } catch {
            Log.session.error("chat decision encode failed: \(error.localizedDescription)")
            return
        }
        TraceLog.log("chat_decision_sent", ["decision": payload], room: session.room.name ?? "")
        _ = try? await session.room.localParticipant.sendText(payload, for: Self.chatDecisionTopic)
    }

    /// Interrupts the current agent generation by sending empty text on
    /// `lk.chat.interrupt` (the LiveKit `Session` API has no `interrupt()`;
    /// this never calls `session.end()`).
    func interrupt() async {
        TraceLog.log("chat_interrupt", room: session.room.name ?? "")
        _ = try? await session.room.localParticipant.sendText("", for: Self.chatInterruptTopic)
    }

    // MARK: - Failure classification

    private func classify(_ error: Session.Error) -> MemoryFailure {
        guard case let .connection(underlying) = error else {
            return .unknown(message: error.localizedDescription)
        }
        return classify(underlying)
    }

    private func classify(_ error: any Error) -> MemoryFailure {
        if let tokenError = error as? MemoryTokenError {
            return .tokenFetch(message: tokenError.localizedDescription)
        }
        if let liveKitError = error as? LiveKitError {
            switch liveKitError.type {
            case .deviceAccessDenied:
                return .micPermissionDenied(agentName: agentDisplayName)
            case .network:
                return .serverUnreachable(message: error.localizedDescription)
            default:
                break
            }
        }
        return .serverUnreachable(message: error.localizedDescription)
    }

    private func classify(agentError: LiveKit.Agent.Error) -> MemoryFailure {
        switch agentError {
        case .timeout:
            .agentNotJoined(agentName: agentDisplayName)
        case .left:
            .unknown(message: agentError.localizedDescription)
        }
    }
}
