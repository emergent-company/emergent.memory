import Foundation

/// Errors thrown by ``SessionLogClient`` while talking to the session log.
enum SessionLogError: LocalizedError, Sendable {
    /// The endpoint answered with a non-2xx status code (401 auth, 503 not
    /// configured, ...).
    case httpStatus(code: Int, message: String)
    /// The endpoint could not be reached at all (DNS/TCP/TLS failure).
    case transport(message: String)
    /// The endpoint answered but the body could not be decoded.
    case decode(message: String)
    /// The configured token endpoint cannot be turned into a session-log URL.
    case invalidConfiguration(message: String)

    var errorDescription: String? {
        switch self {
        case let .httpStatus(code, message):
            "Session log returned HTTP \(code): \(message)"
        case let .transport(message):
            message
        case let .decode(message):
            message
        case let .invalidConfiguration(message):
            message
        }
    }
}

/// One session as listed by `GET /api/sessions`.
///
/// Mirrors the summary shape from `agent/admin.py` `_sessions_summary()`:
/// `{ room, started_at, ended_at, turns, tool_calls, preview }`.
struct SessionSummary: Identifiable, Codable, Equatable, Sendable {
    /// LiveKit room name; also the stable identity of the session.
    let room: String
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

    var id: String { room }

    enum CodingKeys: String, CodingKey {
        case room, turns, preview
        case startedAt = "started_at"
        case endedAt = "ended_at"
        case toolCalls = "tool_calls"
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

    enum CodingKeys: String, CodingKey {
        case kind, role, text, calls, outputs
        case isError = "is_error"
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
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(kind, forKey: .kind)
        try container.encodeIfPresent(role, forKey: .role)
        try container.encodeIfPresent(text, forKey: .text)
        try container.encodeIfPresent(calls, forKey: .calls)
        try container.encodeIfPresent(outputs, forKey: .outputs)
        try container.encodeIfPresent(isError, forKey: .isError)
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
           let value = try? single.decode(String.self) {
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
        guard let raw = try? container.decode(FlexJSON.self, forKey: key) else { return nil }
        return raw.jsonString
    }
}

/// Talks to the session log (`agent/admin.py`): session list and per-room
/// timeline.
///
/// The session log lives on the same host:port as the token endpoint (port
/// 8080), NOT the control plane. The base URL is derived from
/// `config.tokenEndpoint` by stripping the `/api/token` path suffix, e.g.
/// `http://host:8080/api/token` → `http://host:8080`. Read-only, same
/// `X-API-Key` header as the token endpoint.
///
/// Endpoints:
/// - `GET /api/sessions`            → `{"sessions": [SessionSummary]}`
/// - `GET /api/session?room=<room>` → `{"room": ..., "records": [SessionRecord]}`
struct SessionLogClient: Sendable {
    let config: MemoryConfig

    /// The session-log base URL, derived from the token endpoint by stripping
    /// the `/api/token` suffix.
    var sessionsBaseURL: URL? {
        let endpoint = config.tokenEndpoint
        if endpoint.hasSuffix("/api/token") {
            return URL(string: String(endpoint.dropLast("/api/token".count)))
        }
        // Fallback for non-standard endpoints: drop the last two path
        // components (`/api` and `token`) to reach the host root.
        return URL(string: endpoint)?
            .deletingLastPathComponent()
            .deletingLastPathComponent()
    }

    /// Lists recorded sessions, most recently active first.
    func listSessions() async throws -> [SessionSummary] {
        let url = try makeURL(path: "api/sessions", room: nil)
        let data = try await send(url: url)
        let response: SessionsResponse
        do {
            response = try JSONDecoder().decode(SessionsResponse.self, from: data)
        } catch {
            throw SessionLogError.decode(
                message: "Session log returned an unexpected session list: \(error.localizedDescription)"
            )
        }
        return response.sessions
    }

    /// Returns the chronological record timeline for `room`.
    func timeline(room: String) async throws -> [SessionRecord] {
        let url = try makeURL(path: "api/session", room: room)
        let data = try await send(url: url)
        let response: SessionResponse
        do {
            response = try JSONDecoder().decode(SessionResponse.self, from: data)
        } catch {
            throw SessionLogError.decode(
                message: "Session log returned an unexpected session timeline: \(error.localizedDescription)"
            )
        }
        return response.records
    }

    // MARK: - Request building

    /// Builds `<sessionsBaseURL>/<path>?room=<room>` (room optional).
    private func makeURL(path: String, room: String?) throws -> URL {
        guard let base = sessionsBaseURL else {
            throw SessionLogError.invalidConfiguration(
                message: "Could not derive the session-log URL from the token endpoint: \(config.tokenEndpoint)"
            )
        }
        let url = base.appending(path: path)
        guard let room else { return url }
        guard var components = URLComponents(url: url, resolvingAgainstBaseURL: false) else {
            throw SessionLogError.invalidConfiguration(message: "Could not build the session-log URL.")
        }
        components.queryItems = [URLQueryItem(name: "room", value: room)]
        guard let finalURL = components.url else {
            throw SessionLogError.invalidConfiguration(message: "Could not build the session-log URL.")
        }
        return finalURL
    }

    // MARK: - Transport

    /// The `X-API-Key` header is required by the session log, same as the
    /// token endpoint (sent by ``GatewayHTTP``).
    private func send(url: URL) async throws -> Data {
        let data: Data
        let httpResponse: HTTPURLResponse
        do {
            (data, httpResponse) = try await GatewayHTTP.send(
                method: "GET",
                url: url,
                apiKey: config.apiKey
            )
        } catch let transport as GatewayHTTP.TransportError {
            switch transport.reason {
            case let .network(underlying):
                Log.net.error("transport \(url): \(underlying)")
                throw SessionLogError.transport(
                    message: "Could not reach the session log at \(config.tokenEndpoint): \(underlying)"
                )
            case .noHTTPResponse:
                throw SessionLogError.transport(message: "Session log returned no HTTP response.")
            }
        }

        guard (200 ..< 300).contains(httpResponse.statusCode) else {
            let serverMessage = (try? JSONDecoder().decode(ErrorResponse.self, from: data))?.error
                ?? HTTPURLResponse.localizedString(forStatusCode: httpResponse.statusCode)
            Log.net.error("HTTP \(httpResponse.statusCode) \(url.absoluteString): \(serverMessage)")
            throw SessionLogError.httpStatus(code: httpResponse.statusCode, message: serverMessage)
        }
        return data
    }

    // MARK: - Response models

    private struct SessionsResponse: Decodable {
        let sessions: [SessionSummary]
    }

    private struct SessionResponse: Decodable {
        let records: [SessionRecord]
    }

    /// Error body returned by the session log on failure.
    private struct ErrorResponse: Decodable {
        let error: String
    }

}

/// A flexible JSON value used to salvage tool-call arguments/results that the
/// server logs as objects instead of strings.
private enum FlexJSON: Codable, Sendable {
    case string(String)
    case number(Double)
    case bool(Bool)
    case object([String: FlexJSON])
    case array([FlexJSON])
    case null

    /// Compact JSON text for display; plain strings pass through raw.
    var jsonString: String? {
        switch self {
        case let .string(text):
            text
        case let .number(number):
            String(format: "%g", locale: Locale(identifier: "en_US_POSIX"), number)
        case let .bool(value):
            value ? "true" : "false"
        case .null:
            "null"
        case .object, .array:
            (try? JSONEncoder().encode(self)).flatMap { String(data: $0, encoding: .utf8) }
        }
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
            return
        }
        if let value = try? container.decode(String.self) {
            self = .string(value)
            return
        }
        if let value = try? container.decode(Bool.self) {
            self = .bool(value)
            return
        }
        if let value = try? container.decode(Double.self) {
            self = .number(value)
            return
        }
        if let value = try? container.decode([FlexJSON].self) {
            self = .array(value)
            return
        }
        if let value = try? container.decode([String: FlexJSON].self) {
            self = .object(value)
            return
        }
        throw DecodingError.typeMismatch(
            FlexJSON.self,
            DecodingError.Context(
                codingPath: decoder.codingPath,
                debugDescription: "Unsupported JSON value"
            )
        )
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case let .string(value):
            try container.encode(value)
        case let .number(value):
            try container.encode(value)
        case let .bool(value):
            try container.encode(value)
        case let .object(value):
            try container.encode(value)
        case let .array(value):
            try container.encode(value)
        case .null:
            try container.encodeNil()
        }
    }
}
