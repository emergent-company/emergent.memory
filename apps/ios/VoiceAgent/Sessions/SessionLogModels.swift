import Foundation

/// One session as listed by `GET /api/sessions`.
///
/// Mirrors the summary shape from `agent/admin.py` `_sessions_summary()`:
/// `{ room, started_at, ended_at, turns, tool_calls, preview }`.
struct SessionSummary: Identifiable, Codable, Equatable, Sendable {
    /// LiveKit room name; also the stable identity of the session.
    let room: String
    /// How the session started: `manual`, `scheduled`, or `board`.
    var origin: String?
    /// JSON `started_at` (`"HH:mm:ss.SSS"` clock string or null).
    let startedAt: String?
    /// JSON `ended_at` (clock string or null while the session is live).
    let endedAt: String?
    /// Number of recorded turns (user + assistant).
    let turns: Int
    /// Number of recorded tool calls.
    let toolCalls: Int
    /// First user text of the session (truncated server-side to ~160 chars).
    let preview: String?
    /// Agent definition that owns the conversation, when known.
    var agentDefinitionID: String?
    /// Conversation title, when the server has one.
    var title: String?
    /// True when the conversation is archived (hidden from the default list).
    var isArchived: Bool = false

    var id: String {
        room
    }

    enum CodingKeys: String, CodingKey {
        case room, turns, preview, origin, title
        case startedAt = "started_at"
        case endedAt = "ended_at"
        case toolCalls = "tool_calls"
        case agentDefinitionID = "agent_definition_id"
        case isArchived = "is_archived"
    }
}

extension SessionSummary {
    /// Tolerant decoding: new metadata fields (including `is_archived`) may be
    /// absent from older gateways and default to nil/false.
    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        room = try container.decode(String.self, forKey: .room)
        origin = (try? container.decodeIfPresent(String.self, forKey: .origin)) ?? nil
        startedAt = (try? container.decodeIfPresent(String.self, forKey: .startedAt)) ?? nil
        endedAt = (try? container.decodeIfPresent(String.self, forKey: .endedAt)) ?? nil
        turns = (try? container.decodeIfPresent(Int.self, forKey: .turns)) ?? 0
        toolCalls = (try? container.decodeIfPresent(Int.self, forKey: .toolCalls)) ?? 0
        preview = (try? container.decodeIfPresent(String.self, forKey: .preview)) ?? nil
        agentDefinitionID = (try? container.decodeIfPresent(String.self, forKey: .agentDefinitionID)) ?? nil
        title = (try? container.decodeIfPresent(String.self, forKey: .title)) ?? nil
        isArchived = (try? container.decodeIfPresent(Bool.self, forKey: .isArchived)) ?? false
    }
}

/// One pending tool approval of a session (`GET /api/session?room=...`
/// `pending_approvals`). The gateway sends the tool input as a JSON object;
/// it is flattened to a JSON string for display, matching the live approval
/// card's `arguments` field.
struct SessionPendingApproval: Codable, Equatable, Sendable {
    let questionID: String
    let tool: String
    /// Compact JSON text of the approval `input` object.
    let arguments: String

    enum CodingKeys: String, CodingKey {
        case questionID = "questionId"
        case tool
        case arguments
        case input
    }

    init(questionID: String, tool: String, arguments: String) {
        self.questionID = questionID
        self.tool = tool
        self.arguments = arguments
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        questionID = (try? container.decodeIfPresent(String.self, forKey: .questionID)) ?? ""
        tool = (try? container.decodeIfPresent(String.self, forKey: .tool)) ?? ""
        if let text = try? container.decodeIfPresent(String.self, forKey: .arguments) {
            arguments = text
        } else if let raw = try? container.decodeIfPresent(JSONValue.self, forKey: .input) {
            arguments = raw.jsonString ?? ""
        } else {
            arguments = ""
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(questionID, forKey: .questionID)
        try container.encode(tool, forKey: .tool)
        try container.encode(arguments, forKey: .arguments)
    }
}

/// One session's detail (`GET /api/session?room=...`): conversation metadata,
/// pending approvals, and the chronological timeline records.
struct SessionDetail: Equatable, Sendable {
    let room: String
    /// Agent definition that owns the conversation, when known.
    let agentDefinitionID: String?
    /// Conversation title, when the server has one.
    let title: String?
    let pendingApprovals: [SessionPendingApproval]
    let records: [SessionRecord]

    init(
        room: String,
        agentDefinitionID: String? = nil,
        title: String? = nil,
        pendingApprovals: [SessionPendingApproval] = [],
        records: [SessionRecord] = []
    ) {
        self.room = room
        self.agentDefinitionID = agentDefinitionID
        self.title = title
        self.pendingApprovals = pendingApprovals
        self.records = records
    }
}

extension SessionDetail: Decodable {
    private enum CodingKeys: String, CodingKey {
        case room, title, records
        case agentDefinitionID = "agent_definition_id"
        case pendingApprovals = "pending_approvals"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        room = (try? container.decodeIfPresent(String.self, forKey: .room)) ?? ""
        agentDefinitionID = (try? container.decodeIfPresent(String.self, forKey: .agentDefinitionID)) ?? nil
        title = (try? container.decodeIfPresent(String.self, forKey: .title)) ?? nil
        pendingApprovals = (try? container.decodeIfPresent([SessionPendingApproval].self, forKey: .pendingApprovals)) ?? []
        records = (try? container.decodeIfPresent([SessionRecord].self, forKey: .records)) ?? []
    }
}

/// One record of a session timeline (`GET /api/session?room=...`).
///
/// `records` holds the raw `slog` entries from `agent/main_google_realtime.py`
/// (`slog(room, kind, ...)`); `kind` is one of `turn`, `tools_executed`,
/// `usage`, `session_start`, `session_end`, `closing`, `tools_ready`,
/// `instructions_ready`. Only the fields the UI needs are modeled; every other
/// record field (`ts`, `clock`, `variant`, `metrics`, ...) is ignored.
struct SessionRecord: Codable, Equatable, Sendable {
    /// Record kind: `turn` | `tools_executed` | `usage` | `session_start` |
    /// `session_end` | `closing` | ...
    let kind: String
    /// `turn` only: `"user"` or `"assistant"`.
    let role: String?
    /// `turn` only: the transcript text.
    let text: String?
    /// `tools_executed` only: the executed tool calls.
    let calls: [ToolCall]?
    /// `tools_executed` only: the flattened output texts (one per call).
    let outputs: [String]?
    /// True when any `tools_executed` output carried `is_error: true`
    /// (derived at decode time; the server never sets a record-level flag).
    let isError: Bool?
    /// Assistant `turn` only: the reply's grounded references, when it cited
    /// any. Decoded leniently, so an odd element never fails the record.
    let citations: [ChatCitation]?

    enum CodingKeys: String, CodingKey {
        case kind, role, text, calls, outputs, citations
        case isError = "is_error"
    }

    /// Memberwise initializer for callers/tests that build records directly
    /// (decoding uses `init(from:)` below).
    init(
        kind: String,
        role: String? = nil,
        text: String? = nil,
        calls: [ToolCall]? = nil,
        outputs: [String]? = nil,
        isError: Bool? = nil,
        citations: [ChatCitation]? = nil
    ) {
        self.kind = kind
        self.role = role
        self.text = text
        self.calls = calls
        self.outputs = outputs
        self.isError = isError
        self.citations = citations
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        kind = (try? container.decodeIfPresent(String.self, forKey: .kind)) ?? ""
        role = (try? container.decodeIfPresent(String.self, forKey: .role)) ?? nil
        text = (try? container.decodeIfPresent(String.self, forKey: .text)) ?? nil
        calls = (try? container.decodeIfPresent([ToolCall].self, forKey: .calls)) ?? nil

        // The server logs outputs as `[{output, is_error} | null]`
        // (`_on_tools_executed`); flatten to the output texts and surface any
        // error flag at the record level.
        var texts: [String] = []
        var sawError = false
        if let raw = try? container.decodeIfPresent([SessionOutput].self, forKey: .outputs) {
            for item in raw {
                if let outputText = item.text {
                    texts.append(outputText)
                }
                if item.isError {
                    sawError = true
                }
            }
        }
        outputs = texts.isEmpty ? nil : texts
        isError = sawError ? true : nil
        let cited = (try? container.decodeIfPresent(LenientCitations.self, forKey: .citations))?.items ?? []
        citations = cited.isEmpty ? nil : cited
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(kind, forKey: .kind)
        try container.encodeIfPresent(role, forKey: .role)
        try container.encodeIfPresent(text, forKey: .text)
        try container.encodeIfPresent(calls, forKey: .calls)
        try container.encodeIfPresent(outputs, forKey: .outputs)
        try container.encodeIfPresent(isError, forKey: .isError)
        try container.encodeIfPresent(citations, forKey: .citations)
    }
}

/// One element of a `tools_executed` `outputs` array: a plain string, a
/// `{"output": ..., "is_error": <bool>}` object, or null.
private struct SessionOutput: Decodable {
    let text: String?
    let isError: Bool

    private enum Keys: String, CodingKey {
        case output
        case isError = "is_error"
    }

    init(from decoder: Decoder) throws {
        if let single = try? decoder.singleValueContainer(), single.decodeNil() {
            text = nil
            isError = false
            return
        }
        if let single = try? decoder.singleValueContainer(),
           let value = try? single.decode(String.self)
        {
            text = value
            isError = false
            return
        }
        let container = try? decoder.container(keyedBy: Keys.self)
        text = (try? container?.decodeIfPresent(String.self, forKey: .output)) ?? nil
        isError = (try? container?.decodeIfPresent(Bool.self, forKey: .isError)) ?? false
    }
}

/// A tool call logged in a `tools_executed` record.
///
/// The server writes each element as `{"name": ..., "args": <arguments dict>}`
/// (see `_on_tools_executed` in `agent/main_google_realtime.py`), where `args`
/// is a JSON object rather than a string. `arguments` is therefore decoded
/// flexibly (raw string, or object/array serialized to compact JSON) so the
/// UI can show expandable tool-call details. `result`/`is_error` do not appear
/// in this server's `calls` elements but are decoded defensively if present.
struct ToolCall: Codable, Equatable, Sendable {
    let name: String
    /// JSON `args` (or `arguments`): raw text or serialized JSON.
    let arguments: String?
    let result: String?
    /// JSON `is_error` if present.
    let isError: Bool?

    enum CodingKeys: String, CodingKey {
        case name, args, arguments, result
        case isError = "is_error"
    }

    /// Memberwise initializer for callers/tests that build calls directly
    /// (decoding uses `init(from:)` below).
    init(name: String, arguments: String? = nil, result: String? = nil, isError: Bool? = nil) {
        self.name = name
        self.arguments = arguments
        self.result = result
        self.isError = isError
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = (try? container.decodeIfPresent(String.self, forKey: .name)) ?? ""
        arguments = Self.flexibleString(in: container, for: .args)
            ?? Self.flexibleString(in: container, for: .arguments)
        result = Self.flexibleString(in: container, for: .result)
        isError = (try? container.decodeIfPresent(Bool.self, forKey: .isError)) ?? nil
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(name, forKey: .name)
        try container.encodeIfPresent(arguments, forKey: .args)
        try container.encodeIfPresent(result, forKey: .result)
        try container.encodeIfPresent(isError, forKey: .isError)
    }

    /// Decodes `key` as a plain string, or as an object/array serialized back
    /// to compact JSON. Missing or mismatched values decode to nil.
    private static func flexibleString(
        in container: KeyedDecodingContainer<CodingKeys>,
        for key: CodingKeys
    ) -> String? {
        if let text = try? container.decodeIfPresent(String.self, forKey: key) {
            return text
        }
        guard let raw = try? container.decode(JSONValue.self, forKey: key) else { return nil }
        return raw.jsonString
    }
}

// Talks to the session log: session list, per-room detail/timeline, and
// conversation lifecycle actions.
//
// The session log lives on the same host:port as the token endpoint (the
// gateway, e.g. `http://localhost:8095`), NOT the control plane. The base
// URL is derived from `config.tokenEndpoint` by stripping the `/api/token`
// path suffix, e.g. `http://localhost:8095/api/token` →
// `http://localhost:8095`. Same `X-API-Key` header as the token endpoint.
//
// Endpoints:
// - `GET /api/sessions[?agentId=<id>]` → `{"sessions": [SessionSummary]}`
// - `GET /api/session?room=<room>`     → session detail
// - `POST /api/conversations/<id>/archive`
// - `POST /api/conversations/<id>/unarchive`
// - `DELETE /api/conversations/<id>`
//
