import Foundation

// MARK: - History endpoint

/// `GET /api/conversations/:id/history`: the recorded transcript the web chat
/// renders, as ordered items (the gateway's `ConversationHistory`).
///
/// The gateway merges memory's run-history items (messages, tool calls,
/// run boundaries) with the conversation's stored messages and dedupes the
/// overlap, so the client renders one list instead of merging two sources.
struct ConversationHistory: Decodable, Equatable {
    let conversationID: String?
    let items: [HistoryItem]
    let pendingApprovals: [HistoryPendingApproval]

    enum CodingKeys: String, CodingKey {
        case conversationID = "conversation_id"
        case items
        case pendingApprovals = "pending_approvals"
    }

    init(conversationID: String? = nil, items: [HistoryItem], pendingApprovals: [HistoryPendingApproval] = []) {
        self.conversationID = conversationID
        self.items = items
        self.pendingApprovals = pendingApprovals
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        conversationID = try? container.decodeIfPresent(String.self, forKey: .conversationID)
        items = (try? container.decodeIfPresent([HistoryItem].self, forKey: .items)) ?? []
        pendingApprovals = (try? container.decodeIfPresent([HistoryPendingApproval].self, forKey: .pendingApprovals)) ?? []
    }
}

/// One raw history item. The endpoint sends a union of shapes selected by
/// `kind`, so every field is optional and decoding never fails a whole item on
/// one odd field.
struct HistoryItem: Decodable, Equatable {
    let kind: String
    let runID: String?
    let stepNumber: Int?
    let createdAt: String?
    let runStatus: String?
    let runModel: String?
    let completedAt: String?
    let errorMessage: String?
    let role: String?
    let content: HistoryContent?
    let id: String?
    let toolName: String?
    let toolInput: JSONValue?
    let toolOutput: JSONValue?
    let toolStatus: String?
    let durationMs: Int?
    let citations: [ChatCitation]?
    let toolCategory: ChatToolCategory?
    let toolSubject: String?
    let toolView: ServerToolView?

    enum CodingKeys: String, CodingKey {
        case kind, role, content, id, citations
        case runID = "run_id"
        case stepNumber = "step_number"
        case createdAt = "created_at"
        case runStatus = "run_status"
        case runModel = "run_model"
        case completedAt = "completed_at"
        case errorMessage = "error_message"
        case toolName = "tool_name"
        case toolInput = "tool_input"
        case toolOutput = "tool_output"
        case toolStatus = "tool_status"
        case durationMs = "duration_ms"
        case toolCategory = "tool_category"
        case toolSubject = "tool_subject"
        case toolView = "tool_view"
    }

    init(from decoder: Decoder) throws {
        let container = try? decoder.container(keyedBy: CodingKeys.self)
        kind = (try? container?.decodeIfPresent(String.self, forKey: .kind)) ?? ""
        runID = try? container?.decodeIfPresent(String.self, forKey: .runID)
        stepNumber = try? container?.decodeIfPresent(Int.self, forKey: .stepNumber)
        createdAt = try? container?.decodeIfPresent(String.self, forKey: .createdAt)
        runStatus = try? container?.decodeIfPresent(String.self, forKey: .runStatus)
        runModel = try? container?.decodeIfPresent(String.self, forKey: .runModel)
        completedAt = try? container?.decodeIfPresent(String.self, forKey: .completedAt)
        errorMessage = try? container?.decodeIfPresent(String.self, forKey: .errorMessage)
        role = try? container?.decodeIfPresent(String.self, forKey: .role)
        content = try? container?.decodeIfPresent(HistoryContent.self, forKey: .content)
        id = try? container?.decodeIfPresent(String.self, forKey: .id)
        toolName = try? container?.decodeIfPresent(String.self, forKey: .toolName)
        toolInput = try? container?.decodeIfPresent(JSONValue.self, forKey: .toolInput)
        toolOutput = try? container?.decodeIfPresent(JSONValue.self, forKey: .toolOutput)
        toolStatus = try? container?.decodeIfPresent(String.self, forKey: .toolStatus)
        durationMs = try? container?.decodeIfPresent(Int.self, forKey: .durationMs)
        citations = (try? container?.decodeIfPresent([ChatCitation].self, forKey: .citations)) ?? nil
        toolCategory = try? container?.decodeIfPresent(ChatToolCategory.self, forKey: .toolCategory)
        toolSubject = try? container?.decodeIfPresent(String.self, forKey: .toolSubject)
        toolView = try? container?.decodeIfPresent(ServerToolView.self, forKey: .toolView)
    }
}

/// The `content` of a history `message` item.
struct HistoryContent: Decodable, Equatable {
    let text: String?
    let reasoning: String?

    init(text: String?, reasoning: String?) {
        self.text = text
        self.reasoning = reasoning
    }

    init(from decoder: Decoder) throws {
        let container = try? decoder.container(keyedBy: CodingKeys.self)
        text = try? container?.decodeIfPresent(String.self, forKey: .text)
        reasoning = try? container?.decodeIfPresent(String.self, forKey: .reasoning)
    }

    private enum CodingKeys: String, CodingKey {
        case text, reasoning
    }
}

/// One recorded pending approval (`pending_approvals`).
struct HistoryPendingApproval: Decodable, Equatable {
    let questionID: String
    let tool: String
    let input: JSONValue?

    enum CodingKeys: String, CodingKey {
        case questionID = "questionId"
        case tool, input
    }

    init(questionID: String, tool: String, input: JSONValue? = nil) {
        self.questionID = questionID
        self.tool = tool
        self.input = input
    }

    init(from decoder: Decoder) throws {
        let container = try? decoder.container(keyedBy: CodingKeys.self)
        questionID = (try? container?.decodeIfPresent(String.self, forKey: .questionID)) ?? ""
        tool = (try? container?.decodeIfPresent(String.self, forKey: .tool)) ?? ""
        input = try? container?.decodeIfPresent(JSONValue.self, forKey: .input)
    }
}

// MARK: - Fetching

/// Fetching seam for the conversation-history endpoint, so the loader is
/// testable without a network. ``SessionLogClient`` is the live implementation.
protocol ConversationHistoryFetching: Sendable {
    func history(conversationID: String) async throws -> ConversationHistory
}

extension SessionLogClient: ConversationHistoryFetching {}

// MARK: - Recorded JSON access

extension JSONValue {
    var objectValue: [String: JSONValue]? {
        if case let .object(value) = self {
            return value
        }
        return nil
    }

    var arrayValue: [JSONValue]? {
        if case let .array(value) = self {
            return value
        }
        return nil
    }

    var stringValue: String? {
        if case let .string(value) = self {
            return value
        }
        return nil
    }

    var numberValue: Double? {
        if case let .number(value) = self {
            return value
        }
        return nil
    }
}

// MARK: - Dates

/// Lenient date decoding for record timestamps: RFC 3339 with or without
/// fractional seconds, then a plain ISO 8601 date.
///
/// The record's times come from Go's `time.Time`, which marshals RFC 3339 with a
/// variable-length fractional part (`.2Z`, `.500000Z`, `.123456789Z`);
/// `ISO8601DateFormatter` is happiest with milliseconds, so a longer fraction is
/// truncated before the last attempt.
enum TimelineDate {
    static func parse(_ text: String?) -> Date? {
        guard let text, !text.isEmpty else { return nil }
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = fractional.date(from: text) {
            return date
        }
        let plain = ISO8601DateFormatter()
        plain.formatOptions = [.withInternetDateTime]
        if let date = plain.date(from: text) {
            return date
        }
        let normalized = millisecondFraction(of: text)
        return normalized == text ? nil : fractional.date(from: normalized)
    }

    /// `2026-01-01T00:00:01.123456789Z` → `2026-01-01T00:00:01.123Z`; the text
    /// is returned unchanged when it has no fraction or one of at most three
    /// digits.
    static func millisecondFraction(of text: String) -> String {
        guard let dot = text.firstIndex(of: "."),
              let end = text[dot...].firstIndex(where: { $0 == "Z" || $0 == "+" || $0 == "-" })
        else { return text }
        let digits = text[text.index(after: dot) ..< end]
        guard digits.count > 3 else { return text }
        return String(text[..<dot]) + "." + String(digits.prefix(3)) + String(text[end...])
    }
}
