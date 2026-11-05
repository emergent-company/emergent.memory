import AudioToolbox
import AVFoundation
import Combine
import Foundation
import LiveKit

/// Drives a Memory voice session on the LiveKit Swift SDK's
/// `Session`/`LocalMediaModel` architecture: connects + dispatches
/// `alfred` (agent name flows through the token request), classifies
/// connection failures with a retry affordance, and consumes Memory's
/// text-stream signals (`lk.agent.ready` chime, `lk.agent.events` away,
/// `lk.transcription` exit keywords, agent-left end-of-session).
@MainActor
// swiftlint:disable:next type_body_length
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
    let localMedia: LocalMediaModel

    /// Live rich-chat activity (tool chips, thinking, approval/question cards)
    /// decoded from `lk.chat.events` and rendered by `ChatView`.
    ///
    /// `ChatActivityStore` is `@Observable` and this same instance is injected
    /// into the environment by `AgentSessionRoot` so chat surfaces observe it
    /// directly (the controller feeds it via `apply`/`reset`).
    let chatActivity = ChatActivityStore()

    @Published private(set) var phase: Phase = .idle

    /// The Memory conversation id learned from a worker `conversation` event
    /// (sent once when the server first reveals it). Nil until learned.
    @Published var liveConversationID: String?

    /// Injected by the UI layer so a learned conversation id can be persisted
    /// in ``ActiveSessionStore``. Called only when the id changes.
    var onConversationLearned: ((String) -> Void)?

    private let config: MemoryConfig
    /// Conversation to continue, or nil to let the server create a new one.
    let conversationID: String?
    /// Join without pre-connecting the microphone (chat-first sessions).
    private let startMuted: Bool
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
    /// Serializes end-of-session sweeps (exit keyword, agent-left, away) into a
    /// single `endSession` so overlapping triggers don't race each other.
    private var sessionEndTask: Task<Void, Never>?

    /// Speaks agent replies locally (AVSpeechSynthesizer — no server TTS).
    ///
    /// Created on first use: this controller exists from app launch, and
    /// constructing audio objects up front is exactly what used to take audio
    /// away from other apps before the user joined a call.
    private lazy var speechSynthesizer = AVSpeechSynthesizer()

    init(config: MemoryConfig, conversationID: String? = nil, startMuted: Bool = false) {
        self.config = config
        self.conversationID = conversationID
        self.startMuted = startMuted
        let session = Self.makeSession(
            config: config,
            conversationID: conversationID,
            startMuted: startMuted
        )
        self.session = session
        localMedia = LocalMediaModel(session: session)
        observe()
    }

    /// Builds the `Session` with echo cancellation on from the first captured
    /// frame (Memory uses Gemini-native activity detection — no server VAD —
    /// so barge-in depends on client-side AEC).
    private static func makeSession(
        config: MemoryConfig,
        conversationID: String?,
        startMuted: Bool
    ) -> Session {
        let tokenSource = MemoryTokenSource(config: config, conversationID: conversationID)
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
                preConnectAudio: !startMuted,
                agentConnectTimeout: config.agentConnectTimeout
            )
        )
    }

    /// Display name for the configured agent, used in failure messages.
    /// Agent names come from the control plane now (no curated aliases), so
    /// the configured name is the display name.
    var agentDisplayName: String {
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
        TraceLog.log("phase_changed", ["phase": "connecting"], room: session.room.name ?? "") // swiftformat:disable:next redundantSelf
        Log.session.info("start connect agent=\(self.config.agentName) endpoint=\(self.config.tokenEndpoint)")
        registerSignalHandlers()
        phase = .connecting
        seenTranscriptIDs.removeAll()
        lastMessageCount = 0
        agentJoinedTraced = false
        liveConversationID = nil
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
            // A chat-first room must never keep an implicitly-published mic.
            if startMuted, localMedia.isMicrophoneEnabled {
                await localMedia.toggleMicrophone()
            }
            TraceLog.log("livekit_connected", ["room": session.room.name ?? ""], room: session.room.name ?? "") // swiftformat:disable:next redundantSelf
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
                    TraceLog.log("agent_joined", ["identity": agentIdentity], room: session.room.name ?? "")
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
            scheduleSessionEnd()
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
            let count = session.messages.count
            guard count != lastMessageCount else { return }
            lastMessageCount = count
            scanTranscripts()
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
            let messages = session.messages
            let count = messages.count
            if count > lastTracedMessageCount {
                for message in messages.suffix(count - lastTracedMessageCount) {
                    let role: String
                    let text: String
                    switch message.content {
                    case let .userInput(content): role = "userInput"; text = content
                    case let .userTranscript(content): role = "userTranscript"; text = content
                    case let .agentTranscript(content): role = "agentTranscript"; text = content
                    }
                    TraceLog.log("message_received", ["role": role, "text": String(text.prefix(200))], room: session.room.name ?? "")
                    // Drive the live chat-activity turns from message traffic:
                    // user messages open a turn (anchored to the message), the
                    // first agent transcript of the turn dismisses the working
                    // line and arms the stop-quiet timer.
                    switch message.content {
                    case .userInput(_), .userTranscript:
                        if message.isFinal {
                            chatActivity.userTurnStarted(anchorMessageID: message.id)
                        }
                    case .agentTranscript:
                        chatActivity.agentReplyStarted()
                    }
                    // Speak agent replies locally (free, client-side TTS) only
                    // when the worker is text-only (`ttsStrategy == "client"`);
                    // a `"server"` worker streams its own TTS audio.
                    if case let .agentTranscript(agentText) = message.content, !agentText.isEmpty, config.ttsStrategy == "client" {
                        let utterance = AVSpeechUtterance(string: agentText)
                        utterance.voice = AVSpeechSynthesisVoice(language: "en-US")
                        speechSynthesizer.speak(utterance)
                    }
                }
                lastTracedMessageCount = count
            }
        }
    }

    /// Coalesces session-end triggers into a single `endSession` call, mirroring
    /// `scheduleTranscriptScan`/`scheduleMessageTrace`: cancel/replace instead
    /// of spawning overlapping sweeps.
    func scheduleSessionEnd() {
        sessionEndTask?.cancel()
        sessionEndTask = Task { @MainActor [weak self] in
            guard let self else { return }
            await endSession()
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
                scheduleSessionEnd()
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
        Task { [weak self] in
            guard let self else { return }
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
        Task { [weak self] in
            guard let self else { return }
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
        Task { [weak self] in
            guard let self else { return }
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
}
