import Foundation
import Observation

// MARK: - Streaming seam

/// Opens the gateway's chat stream and yields its raw SSE `data:` payloads.
///
/// A seam rather than a concrete client so the transport (and its frame
/// mapping) is testable without a network: tests feed recorded frames.
protocol ChatStreaming: Sendable {
    func frames(
        conversationID: String?,
        agentDefinitionID: String?,
        message: String
    ) -> AsyncThrowingStream<String, Error>
}

/// The live implementation: `POST {gateway}/api/chat` with the device
/// credential, `Accept: text/event-stream`.
///
/// This is the same endpoint the web chat uses. The app used to reach text only
/// through a LiveKit room, which needed a token endpoint, a LiveKit server and a
/// bridge worker — none of which exist on a deployment without a voice stack.
struct HTTPChatStreamClient: ChatStreaming {
    let config: MemoryConfig
    var session: URLSession = .shared

    func frames(
        conversationID: String?,
        agentDefinitionID: String?,
        message: String
    ) -> AsyncThrowingStream<String, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    let request = try makeRequest(
                        conversationID: conversationID,
                        agentDefinitionID: agentDefinitionID,
                        message: message
                    )
                    let (bytes, response) = try await session.bytes(for: request)
                    guard let http = response as? HTTPURLResponse else {
                        throw GatewayError.transport("The chat stream returned no HTTP response.")
                    }
                    guard (200 ..< 300).contains(http.statusCode) else {
                        var body = Data()
                        for try await line in bytes.lines {
                            body.append(Data(line.utf8))
                        }
                        throw GatewayError.httpStatus(http.statusCode, Self.errorMessage(body, status: http.statusCode))
                    }

                    var parser = SSEFrameParser()
                    for try await line in bytes.lines {
                        if Task.isCancelled { break }
                        if let payload = parser.consume(line) {
                            continuation.yield(payload)
                        }
                    }
                    if !Task.isCancelled, let payload = parser.finish() {
                        continuation.yield(payload)
                    }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    private func makeRequest(
        conversationID: String?,
        agentDefinitionID: String?,
        message: String
    ) throws -> URLRequest {
        guard let base = config.gatewayBaseURL else {
            throw GatewayError.invalidConfiguration(
                "Could not derive the chat URL from the token endpoint: \(config.tokenEndpoint)"
            )
        }
        let url = base.appending(path: "api").appending(path: "chat")
        let body = try JSONEncoder().encode(ChatBody(
            agentDefinitionId: agentDefinitionID,
            conversationId: conversationID,
            message: message
        ))
        return GatewayHTTP.makeRequest(
            method: "POST",
            url: url,
            apiKey: config.apiKey,
            body: body,
            contentType: "application/json",
            extraHeaders: [
                "Accept": "text/event-stream",
                "Cache-Control": "no-cache",
            ],
            timeout: 600
        )
    }

    /// Mirrors the gateway's `{"error": "..."}` body when present.
    private static func errorMessage(_ body: Data, status: Int) -> String {
        if let object = try? JSONSerialization.jsonObject(with: body) as? [String: Any],
           let message = object["error"] as? String ?? object["detail"] as? String
        {
            return "Chat returned HTTP \(status): \(message)"
        }
        return "Chat returned HTTP \(status): \(HTTPURLResponse.localizedString(forStatusCode: status))"
    }

    private struct ChatBody: Encodable {
        let agentDefinitionId: String?
        let conversationId: String?
        let message: String

        enum CodingKeys: String, CodingKey {
            case agentDefinitionId
            case conversationId
            case message
        }

        func encode(to encoder: Encoder) throws {
            var container = encoder.container(keyedBy: CodingKeys.self)
            try container.encodeIfPresent(agentDefinitionId, forKey: .agentDefinitionId)
            try container.encodeIfPresent(conversationId, forKey: .conversationId)
            try container.encode(message, forKey: .message)
        }
    }
}

// MARK: - Transport

/// Serves a text turn over the gateway's HTTP chat stream and feeds the same
/// transcript/activity/A2UI stores the LiveKit voice path feeds.
///
/// Nothing here mints a token, joins a room or needs a worker: text works on a
/// project with voice disabled and on a deployment with no LiveKit stack.
@MainActor
@Observable
final class HTTPChatTransport {
    /// True while a turn is streaming (drives the working line and stop button).
    private(set) var isStreaming = false
    /// The last turn-level failure, surfaced at the composer.
    private(set) var errorMessage: String?
    /// The open conversation: the launched one, or the id the `meta` frame
    /// reported for a new chat.
    private(set) var conversationID: String?

    /// Called once when a new conversation's id is first learned, so the UI can
    /// persist it in ``ActiveSessionStore``.
    var onConversationLearned: ((String) -> Void)?

    private let config: MemoryConfig
    private let stream: any ChatStreaming
    private let urlSession: URLSession
    private var turnTask: Task<Void, Never>?
    /// Live transcript fed to the view (user turn + streamed reply).
    private var liveMessages: [LiveTranscriptMessage] = []
    private var fallbackToolCounter = 0
    /// Resolved agent definition id for the selected agent name (cached).
    private var agentDefinitionID: String?
    private var agentDefinitionResolved = false

    init(
        config: MemoryConfig = MemoryConfig(),
        stream: (any ChatStreaming)? = nil,
        session: URLSession = .shared
    ) {
        self.config = config
        self.stream = stream ?? HTTPChatStreamClient(config: config, session: session)
        urlSession = session
    }

    // MARK: - Turn lifecycle

    /// Sends `text` and streams the reply into `transcript`/`activity`.
    ///
    /// Returns immediately with the turn's task (so tests, and anything else
    /// that needs to, can await it) while ``stop()`` can cancel it. One turn at
    /// a time — a second send while streaming is dropped.
    @discardableResult
    func send(
        _ text: String,
        conversationID: String?,
        agentName: String,
        transcript: ChatSessionTranscriptStore,
        activity: ChatActivityStore
    ) -> Task<Void, Never>? {
        let message = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !message.isEmpty, !isStreaming else { return nil }
        if let conversationID, !conversationID.isEmpty {
            self.conversationID = conversationID
        }
        errorMessage = nil
        isStreaming = true

        let userMessageID = "http-user-\(UUID().uuidString)"
        liveMessages.append(LiveTranscriptMessage(id: userMessageID, role: .user, text: message))
        transcript.syncLive(liveMessages)
        activity.userTurnStarted(anchorMessageID: userMessageID)

        let task = Task { @MainActor [weak self] in
            guard let self else { return }
            await runTurn(
                message: message,
                agentName: agentName,
                transcript: transcript,
                activity: activity,
                assistantMessageID: "http-assistant-\(UUID().uuidString)"
            )
        }
        turnTask = task
        return task
    }

    /// Stops the turn: cancels the stream, marks the reply finished, and asks
    /// the server to cancel the run (its id comes from the conversation
    /// timeline — the stream never carries one).
    func stop(activity: ChatActivityStore) async {
        turnTask?.cancel()
        turnTask = nil
        isStreaming = false
        activity.stopGenerating()

        guard let conversationID else { return }
        guard let runID = await activeRunID(conversationID: conversationID) else { return }
        await cancelRun(runID)
    }

    /// Answers a pending approval or question. The gateway resumes the paused
    /// run; the caller reloads the transcript to see the continuation.
    func respond(questionID: String, response: String, message: String) async throws {
        try await SessionLogClient(config: config, session: urlSession).respondQuestion(
            questionID: questionID,
            response: response,
            message: message
        )
    }

    /// Drops the live transcript for a new session.
    func reset() {
        turnTask?.cancel()
        turnTask = nil
        isStreaming = false
        errorMessage = nil
        liveMessages.removeAll()
        conversationID = nil
    }

    /// Reloads the live transcript into the view (used after the view resets).
    func refreshLiveTranscript(_ transcript: ChatSessionTranscriptStore) {
        transcript.syncLive(liveMessages)
    }

    // MARK: - Streaming

    private func runTurn(
        message: String,
        agentName: String,
        transcript: ChatSessionTranscriptStore,
        activity: ChatActivityStore,
        assistantMessageID: String
    ) async {
        var assistantText = ""
        var assistantStarted = false

        do {
            let agentID = await resolveAgentDefinitionID(name: agentName)
            let frames = stream.frames(
                conversationID: conversationID,
                agentDefinitionID: agentID,
                message: message
            )
            for try await payload in frames {
                if Task.isCancelled { break }
                guard let event = ChatStreamFrameDecoder.decode(
                    payload,
                    fallbackToolID: { [weak self] in
                        guard let self else { return "tool" }
                        fallbackToolCounter += 1
                        return "http-tool-\(fallbackToolCounter)"
                    }
                ) else { continue }
                apply(
                    event,
                    assistantMessageID: assistantMessageID,
                    assistantText: &assistantText,
                    assistantStarted: &assistantStarted,
                    transcript: transcript,
                    activity: activity
                )
            }
            if !Task.isCancelled {
                activity.markReplyFinished()
            }
        } catch is CancellationError {
            // Stop pressed: keep whatever streamed in.
        } catch {
            guard !Task.isCancelled else { return }
            errorMessage = Self.message(for: error)
            activity.markReplyFinished()
        }
        isStreaming = false
    }

    private func apply(
        _ event: ChatStreamEvent,
        assistantMessageID: String,
        assistantText: inout String,
        assistantStarted: inout Bool,
        transcript: ChatSessionTranscriptStore,
        activity: ChatActivityStore
    ) {
        switch event {
        case let .meta(learnedConversationID, _):
            guard let learnedConversationID, learnedConversationID != conversationID else { return }
            conversationID = learnedConversationID
            onConversationLearned?(learnedConversationID)
        case let .text(delta):
            if !assistantStarted {
                assistantStarted = true
                activity.agentReplyStarted()
            }
            assistantText += delta
            upsertAssistant(assistantMessageID, text: assistantText, transcript: transcript)
        case let .activity(chatEvent):
            activity.apply(chatEvent)
        case .citations:
            break
        case let .failure(message):
            errorMessage = message
            activity.markReplyFinished()
        case .done:
            activity.markReplyFinished()
        case let .replay(events):
            for event in events {
                apply(
                    event,
                    assistantMessageID: assistantMessageID,
                    assistantText: &assistantText,
                    assistantStarted: &assistantStarted,
                    transcript: transcript,
                    activity: activity
                )
            }
        }
    }

    private func upsertAssistant(
        _ id: String,
        text: String,
        transcript: ChatSessionTranscriptStore
    ) {
        if let index = liveMessages.firstIndex(where: { $0.id == id }) {
            liveMessages[index] = LiveTranscriptMessage(id: id, role: .assistant, text: text)
        } else {
            liveMessages.append(LiveTranscriptMessage(id: id, role: .assistant, text: text))
        }
        transcript.syncLive(liveMessages)
    }

    // MARK: - Agent definition

    /// Resolves the selected agent's definition id, which the chat request
    /// needs to talk to the agent the user picked. Best-effort: when the lookup
    /// fails the request omits it and the server uses the project default.
    private func resolveAgentDefinitionID(name: String) async -> String? {
        if agentDefinitionResolved { return agentDefinitionID }
        agentDefinitionResolved = true
        guard !name.isEmpty else { return nil }
        do {
            let agents = try await ControlPlaneClient(config: config).listAgents()
            agentDefinitionID = agents.first { $0.name == name }?.id
        } catch {
            agentDefinitionID = nil
        }
        return agentDefinitionID
    }

    // MARK: - Run cancellation

    /// The newest run id in the conversation, read from the conversation
    /// timeline (`GET /api/conversations/{id}/history`, whose `run_start` /
    /// `run_end` items carry `run_id`). Nil when no run has been persisted yet,
    /// in which case there is nothing to cancel.
    private func activeRunID(conversationID: String) async -> String? {
        guard let base = config.gatewayBaseURL else { return nil }
        let url = base
            .appending(path: "api")
            .appending(path: "conversations")
            .appending(path: conversationID)
            .appending(path: "history")
        let request = GatewayHTTP.makeRequest(method: "GET", url: url, apiKey: config.apiKey)
        guard let data = try? await GatewayHTTP.sendChecked(
            request,
            service: GatewayServiceMessages(
                httpPrefix: "Chat history",
                noResponsePrefix: "Chat history",
                reachTarget: "the chat history at \(config.tokenEndpoint)"
            ),
            session: urlSession
        ) else { return nil }
        guard let response = try? JSONDecoder().decode(ConversationHistory.self, from: data) else {
            return nil
        }
        return response.items.last { $0.runID?.isEmpty == false }?.runID
    }

    private func cancelRun(_ runID: String) async {
        guard let base = config.gatewayBaseURL else { return }
        let url = base
            .appending(path: "api")
            .appending(path: "chat")
            .appending(path: "runs")
            .appending(path: runID)
            .appending(path: "cancel")
        let request = GatewayHTTP.makeRequest(
            method: "POST",
            url: url,
            apiKey: config.apiKey,
            body: Data("{}".utf8),
            contentType: "application/json"
        )
        _ = try? await GatewayHTTP.sendChecked(
            request,
            service: GatewayServiceMessages(
                httpPrefix: "Stop",
                noResponsePrefix: "Stop",
                reachTarget: "the chat at \(config.tokenEndpoint)"
            ),
            session: urlSession
        )
    }

    // MARK: - Failure messages

    /// Message for a failed turn. A misconfigured deployment is reported as
    /// configuration, never as an unreachable server.
    private static func message(for error: Error) -> String {
        guard let gateway = error as? GatewayError else {
            return error.localizedDescription
        }
        switch gateway {
        case let .httpStatus(code, message):
            switch code {
            case 401, 403:
                return "This device's credential was rejected. Re-scan the setup code from the web app."
            case 502, 503:
                return "The Memory service is not reachable right now. Your message was not sent."
            default:
                return message ?? "Chat failed with HTTP \(code)."
            }
        case .invalidConfiguration, .transport, .decode:
            return gateway.localizedDescription
        }
    }

    private struct ConversationHistory: Decodable {
        struct Item: Decodable {
            let kind: String?
            let runID: String?

            enum CodingKeys: String, CodingKey {
                case kind
                case runID = "run_id"
            }
        }

        let items: [Item]
    }
}
