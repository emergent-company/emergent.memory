import Foundation

// MARK: - Gateway chat stream (`POST /api/chat`, SSE)

/// One decoded frame from the gateway chat stream.
///
/// The gateway forwards memory's SSE events and rewrites a few of them (see
/// `gateway/sse_markdown.go`): `token` stays a raw markdown delta, `html` is the
/// authoritative snapshot emitted just before `done`, and `ask_user` tool calls
/// become `question` frames. Everything the LiveKit worker path delivers on
/// `lk.chat.events` arrives here as ordinary frames instead.
enum ChatStreamEvent: Equatable {
    /// Conversation identity, plus the run id when the server already has one.
    case meta(conversationID: String?, runID: String?)
    /// One raw markdown delta of the reply.
    case text(String)
    /// Tool/thinking/approval/question/A2UI activity, mapped onto the same
    /// events the LiveKit worker path feeds the activity store.
    case activity(ChatEvent)
    /// The turn's grounded citations, drawn as the reply's Sources footer.
    case citations([ChatCitation])
    /// A terminal failure for the turn.
    case failure(String)
    /// The turn finished.
    case done
    /// Retained in-flight frames replayed for a stream this client did not run.
    case replay([ChatStreamEvent])
}

/// Incremental Server-Sent-Events parser: feed it one line at a time and it
/// returns the joined `data:` payload on every dispatch (blank line).
///
/// Comments (`: ping` keepalives) and other fields are ignored, matching the
/// browser's `EventSource` semantics closely enough for this stream.
struct SSEFrameParser {
    private var dataLines: [String] = []

    /// Feeds `line` (without its trailing newline). Returns the payload when
    /// the line dispatches an event.
    mutating func consume(_ line: String) -> String? {
        if line.isEmpty {
            return dispatch()
        }
        if line.hasPrefix(":") {
            return nil
        }
        guard let separator = line.firstIndex(of: ":") else {
            return nil
        }
        let field = String(line[line.startIndex ..< separator])
        var value = String(line[line.index(after: separator)...])
        if value.hasPrefix(" ") {
            value.removeFirst()
        }
        if field == "data" {
            dataLines.append(value)
        }
        return nil
    }

    /// Flushes a payload when the connection ends without a trailing blank
    /// line.
    mutating func finish() -> String? {
        dispatch()
    }

    private mutating func dispatch() -> String? {
        guard !dataLines.isEmpty else { return nil }
        let payload = dataLines.joined(separator: "\n")
        dataLines.removeAll(keepingCapacity: true)
        return payload.isEmpty ? nil : payload
    }
}

/// Decodes one SSE `data:` payload into a ``ChatStreamEvent``.
enum ChatStreamFrameDecoder {
    /// Decodes `payload`. `fallbackToolID` supplies an id for a tool frame the
    /// server sent without one, so a start/end pair can still be correlated.
    static func decode(
        _ payload: String,
        fallbackToolID: () -> String
    ) -> ChatStreamEvent? {
        guard let data = payload.data(using: .utf8) else { return nil }
        return decode(data: data, fallbackToolID: fallbackToolID)
    }

    static func decode(
        data: Data,
        fallbackToolID: () -> String
    ) -> ChatStreamEvent? {
        let decoder = JSONDecoder()
        guard let envelope = try? decoder.decode(ChatStreamEnvelope.self, from: data) else {
            return nil
        }
        switch envelope.type {
        case "meta", "token", "html", "thinking":
            return contentEvent(envelope)
        case "mcp_tool", "approval", "question":
            return interactionEvent(envelope, fallbackToolID: fallbackToolID)
        case "ui", "citations", "error", "done":
            return surfaceEvent(envelope)
        case "subagent":
            return subagentEvent(envelope)
        case "live_replay":
            return replay(data: data, fallbackToolID: fallbackToolID)
        default:
            // Unknown frame kind — ignored so the stream keeps working when the
            // server adds new ones.
            return nil
        }
    }

    /// The session envelope, markdown deltas and thinking segments.
    private static func contentEvent(_ envelope: ChatStreamEnvelope) -> ChatStreamEvent? {
        switch envelope.type {
        case "meta":
            return .meta(
                conversationID: envelope.conversationId?.nonEmpty,
                runID: envelope.runId?.nonEmpty
            )
        case "token":
            guard let token = envelope.token, !token.isEmpty else { return nil }
            return .text(token)
        case "html":
            // The authoritative snapshot is rendered from the accumulated
            // `token` deltas instead; the snapshot exists for hosts that render
            // server-side HTML.
            return nil
        default:
            // A segment-closing frame (`done: true`) carries no new text, but it
            // still has to reach the store: it is what stops the live shimmer.
            // It is not a boundary — a later delta before an activity boundary
            // appends to the same row and turns the shimmer back on.
            if envelope.done == true {
                return .activity(.thinkingDone(id: envelope.id?.nonEmpty))
            }
            guard let text = envelope.text, !text.isEmpty else {
                // An empty frame that is not a close says nothing.
                return nil
            }
            return .activity(.thinking(id: envelope.id?.nonEmpty, delta: text))
        }
    }

    /// A live child/subagent run projected onto the parent chat stream.
    private static func subagentEvent(_ envelope: ChatStreamEnvelope) -> ChatStreamEvent? {
        let id = envelope.id?.nonEmpty ?? envelope.runId?.nonEmpty
        guard let id else { return nil }
        return .activity(.subagent(ChatSubagentEvent(
            id: id,
            parentRunId: envelope.parentRunId?.nonEmpty,
            runId: envelope.runId?.nonEmpty,
            agentName: envelope.agentName?.nonEmpty,
            task: envelope.task?.nonEmpty,
            status: envelope.status?.nonEmpty,
            error: envelope.error?.nonEmpty
        )))
    }

    /// The cards a turn can pause on: tool activity, approvals and questions.
    private static func interactionEvent(
        _ envelope: ChatStreamEnvelope,
        fallbackToolID: () -> String
    ) -> ChatStreamEvent? {
        switch envelope.type {
        case "mcp_tool":
            return toolEvent(envelope, fallbackToolID: fallbackToolID)
        case "approval":
            guard let questionID = envelope.questionId?.nonEmpty else { return nil }
            return .activity(.approval(ChatApprovalEvent(
                questionId: questionID,
                tool: envelope.tool ?? "",
                arguments: envelope.input?.displayText ?? ""
            )))
        default:
            return questionEvent(envelope)
        }
    }

    private static func toolEvent(
        _ envelope: ChatStreamEnvelope,
        fallbackToolID: () -> String
    ) -> ChatStreamEvent? {
        guard let tool = envelope.tool, !tool.isEmpty else { return nil }
        let id = envelope.id?.nonEmpty ?? fallbackToolID()
        if envelope.status == "started" {
            return .activity(.toolCall(ChatToolCallEvent(
                id: id,
                tool: tool,
                arguments: envelope.result?.displayText ?? "",
                category: envelope.toolCategory,
                subject: envelope.toolSubject
            )))
        }
        let failed = envelope.status == "error" || envelope.status == "failed"
        // The size is the output payload's, measured before the error/status
        // fallback and in the form the web console measures (a string as-is,
        // anything else as JSON): "129 B response" must describe what the tool
        // returned, not the message explaining that it returned nothing.
        let outputValue = envelope.result
        let output = outputValue?.displayText
        return .activity(.toolResult(ChatToolResultEvent(
            id: id,
            tool: tool,
            result: envelope.error?.nonEmpty ?? output ?? envelope.status ?? "",
            isError: failed,
            category: envelope.toolCategory,
            view: envelope.toolView,
            outputSize: ToolResultSize.byteCount(of: outputValue?.wireText)
        )))
    }

    private static func questionEvent(_ envelope: ChatStreamEnvelope) -> ChatStreamEvent? {
        guard let questionID = envelope.questionId?.nonEmpty else { return nil }
        let options = (envelope.options ?? []).map { option in
            ChatQuestionOption(
                label: option.label,
                value: option.value,
                description: option.description
            )
        }
        return .activity(.question(ChatQuestionEvent(
            questionId: questionID,
            question: envelope.question ?? "",
            interactionType: Self.interactionType(envelope.interactionType),
            options: options,
            placeholder: envelope.placeholder?.nonEmpty,
            maxLength: envelope.maxLength
        )))
    }

    /// Rendered surfaces and the turn's terminal frames.
    private static func surfaceEvent(_ envelope: ChatStreamEnvelope) -> ChatStreamEvent? {
        switch envelope.type {
        case "ui":
            guard let surfaceID = envelope.surfaceId?.nonEmpty,
                  let messages = envelope.messages
            else { return nil }
            return .activity(.ui(ChatUISurfaceEvent(
                surfaceId: surfaceID,
                messages: messages
            )))
        case "citations":
            return .citations(envelope.citations?.items ?? [])
        case "error":
            return .failure(envelope.error?.nonEmpty ?? "The agent hit an error.")
        default:
            return .done
        }
    }

    /// `live_replay` re-runs the per-frame switch over its in-order `frames`
    /// array, so retained in-flight state renders exactly like the live path.
    private static func replay(
        data: Data,
        fallbackToolID: () -> String
    ) -> ChatStreamEvent? {
        guard
            let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
            let frames = object["frames"] as? [Any]
        else { return nil }
        var events: [ChatStreamEvent] = []
        for frame in frames {
            guard let frameData = try? JSONSerialization.data(withJSONObject: frame),
                  let event = decode(data: frameData, fallbackToolID: fallbackToolID)
            else { continue }
            if case let .replay(nested) = event {
                events.append(contentsOf: nested)
            } else {
                events.append(event)
            }
        }
        return events.isEmpty ? nil : .replay(events)
    }

    /// The stream's question kinds differ from the worker's: HTTP uses `text`
    /// where `lk.chat.events` uses `free_text`.
    private static func interactionType(_ raw: String?) -> QuestionInteractionType {
        switch raw {
        case "text", "free_text": .freeText
        case "multi_select": .multiSelect
        default: .buttons
        }
    }
}

/// The union of the fields the gateway's frames carry. Every field is optional
/// except `type`; each frame kind reads only its own.
private struct ChatStreamEnvelope: Decodable {
    struct Option: Decodable {
        let label: String
        let value: String
        let description: String?
    }

    let type: String
    let conversationId: String?
    let runId: String?
    let parentRunId: String?
    let agentName: String?
    let task: String?
    let token: String?
    let text: String?
    let done: Bool?
    let id: String?
    let tool: String?
    let status: String?
    let input: ChatUIPropValue?
    let result: ChatUIPropValue?
    let error: String?
    let questionId: String?
    let question: String?
    let interactionType: String?
    let options: [Option]?
    let placeholder: String?
    let maxLength: Int?
    let surfaceId: String?
    let messages: [ChatUIMessage]?
    /// Decoded leniently: the `meta` frame carries the key too, and an odd
    /// element must never fail the whole frame.
    let citations: LenientCitations?
    /// The gateway's presentation of a tool call: its category (verb per tense,
    /// family, glyph), the one-line subject of what it was asked to do, and the
    /// readable view of its result. Sent on `mcp_tool` frames so the app renders
    /// the call the way the web console does rather than re-deriving it.
    let toolCategory: ChatToolCategory?
    let toolSubject: String?
    let toolView: ServerToolView?
}

private extension String {
    /// The value when it is not empty, so an empty `""` reads as absent.
    var nonEmpty: String? {
        isEmpty ? nil : self
    }
}
