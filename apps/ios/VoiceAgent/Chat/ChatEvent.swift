import Foundation

// MARK: - Worker → client chat events (`lk.chat.events`)

/// A rich chat activity event decoded from the Memory worker's
/// `lk.chat.events` text stream. Each LiveKit text-stream message carries ONE
/// JSON object; unknown `type` values are dropped (returns `nil`), so older
/// workers or future event kinds degrade gracefully.
enum ChatEvent: Equatable {
    case toolCall(ChatToolCallEvent)
    case toolResult(ChatToolResultEvent)
    /// One delta of the current thinking/reasoning block; the store
    /// accumulates deltas into a single block per turn.
    case thinking(delta: String)
    case approval(ChatApprovalEvent)
    case question(ChatQuestionEvent)
}

/// A live tool invocation (`{"type":"tool_call",...}`). `id` is worker-assigned
/// (`t1`, `t2`, ...) and correlates with its `tool_result`.
struct ChatToolCallEvent: Equatable, Decodable {
    let id: String
    let tool: String
    let arguments: String
}

/// The completion of a previously announced `tool_call` with the same `id`.
struct ChatToolResultEvent: Equatable, Decodable {
    let id: String
    let tool: String
    let result: String
    let isError: Bool
}

/// An approval request: the run is paused until the user approves or rejects
/// the tool call.
struct ChatApprovalEvent: Equatable, Decodable {
    let questionId: String
    let tool: String
    let arguments: String
}

/// How a question card accepts input. The worker normalizes the value before
/// sending it; unknown values make the whole event undecodable (dropped).
enum QuestionInteractionType: String, Equatable, Decodable {
    case buttons
    case multiSelect = "multi_select"
    case freeText = "free_text"
}

/// One selectable option of a buttons/multi-select question.
struct ChatQuestionOption: Equatable, Decodable {
    let label: String
    let value: String
    let description: String?
}

/// An `ask_user` question rendered as an interactive card.
struct ChatQuestionEvent: Equatable, Decodable {
    let questionId: String
    let question: String
    let interactionType: QuestionInteractionType
    let options: [ChatQuestionOption]
    let placeholder: String?
    let maxLength: Int?

    /// Decodes options/placeholder defensively: some workers omit empty
    /// option arrays or leave placeholder blank.
    enum CodingKeys: String, CodingKey {
        case questionId
        case question
        case interactionType
        case options
        case placeholder
        case maxLength
    }

    /// Internal memberwise initializer (decoding uses `init(from:)`).
    init(questionId: String, question: String, interactionType: QuestionInteractionType,
         options: [ChatQuestionOption], placeholder: String?, maxLength: Int?) {
        self.questionId = questionId
        self.question = question
        self.interactionType = interactionType
        self.options = options
        self.placeholder = placeholder
        self.maxLength = maxLength
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        questionId = try container.decode(String.self, forKey: .questionId)
        question = try container.decode(String.self, forKey: .question)
        interactionType = try container.decode(QuestionInteractionType.self, forKey: .interactionType)
        options = (try? container.decode([ChatQuestionOption].self, forKey: .options)) ?? []
        placeholder = (try? container.decodeIfPresent(String.self, forKey: .placeholder)) ?? nil
        maxLength = (try? container.decodeIfPresent(Int.self, forKey: .maxLength)) ?? nil
    }
}

// MARK: - Decoding

/// Decodes one `lk.chat.events` payload. Returns `nil` for malformed JSON or
/// unknown `type` values (silently ignored, per protocol).
func decodeChatEvent(_ json: String) -> ChatEvent? {
    guard let data = json.data(using: .utf8) else { return nil }
    return decodeChatEvent(data: data)
}

func decodeChatEvent(data: Data) -> ChatEvent? {
    let decoder = JSONDecoder()
    guard
        let envelope = try? decoder.decode(ChatEventEnvelope.self, from: data)
    else { return nil }

    switch envelope.type {
    case "tool_call":
        guard let event = try? decoder.decode(ChatToolCallEvent.self, from: data) else { return nil }
        return .toolCall(event)
    case "tool_result":
        guard let event = try? decoder.decode(ChatToolResultEvent.self, from: data) else { return nil }
        return .toolResult(event)
    case "thinking":
        guard let event = try? decoder.decode(ChatThinkingEvent.self, from: data) else { return nil }
        return .thinking(delta: event.delta)
    case "approval":
        guard let event = try? decoder.decode(ChatApprovalEvent.self, from: data) else { return nil }
        return .approval(event)
    case "question":
        guard let event = try? decoder.decode(ChatQuestionEvent.self, from: data) else { return nil }
        return .question(event)
    default:
        // Unknown event type — ignored silently.
        return nil
    }
}

private struct ChatEventEnvelope: Decodable {
    let type: String
}

private struct ChatThinkingEvent: Decodable {
    let delta: String
}

// MARK: - Client → worker decisions (`lk.chat.decision`)

/// A decision the user made on an interactive card, sent to the worker as one
/// JSON object on `lk.chat.decision`.
enum ChatDecision: Equatable {
    case approve(questionId: String)
    case reject(questionId: String, reason: String?)
    case answer(questionId: String, value: String)
}

/// JSON payload for `lk.chat.decision`. Encoded fields depend on the kind:
/// - approve: `{type:"approval", questionId, action:"approve"}`
/// - reject:  `{type:"approval", questionId, action:"reject", message:"<reason>"}`
/// - answer:  `{type:"question", questionId, answer:"<value>"}`
struct ChatDecisionPayload: Encodable, Equatable {
    let type: String
    let questionId: String
    let action: String?
    let message: String?
    let answer: String?

    init(type: String, questionId: String, action: String? = nil, message: String? = nil, answer: String? = nil) {
        self.type = type
        self.questionId = questionId
        self.action = action
        self.message = message
        self.answer = answer
    }
}

/// Encodes a decision to the exact JSON bytes sent on `lk.chat.decision`.
func makeChatDecisionPayload(_ decision: ChatDecision) -> ChatDecisionPayload {
    switch decision {
    case let .approve(questionId):
        ChatDecisionPayload(type: "approval", questionId: questionId, action: "approve")
    case let .reject(questionId, reason):
        ChatDecisionPayload(type: "approval", questionId: questionId, action: "reject", message: reason)
    case let .answer(questionId, value):
        ChatDecisionPayload(type: "question", questionId: questionId, answer: value)
    }
}

/// Serializes a decision to the JSON string sent over `lk.chat.decision`.
func encodeChatDecision(_ decision: ChatDecision) throws -> String {
    let data = try JSONEncoder().encode(makeChatDecisionPayload(decision))
    guard let text = String(data: data, encoding: .utf8) else {
        throw EncodingError.invalidValue(decision, EncodingError.Context(
            codingPath: [],
            debugDescription: "Decision did not serialize to UTF-8 text."
        ))
    }
    return text
}
