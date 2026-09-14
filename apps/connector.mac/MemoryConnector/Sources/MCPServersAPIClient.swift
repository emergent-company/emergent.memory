import Foundation

// MARK: - Errors

/// Errors surfaced by `MCPServersAPIClient`.
///
/// The connector's loopback management API returns JSON `{"error": "..."}` for
/// 4xx/5xx responses; that message is surfaced verbatim as
/// `.server(status, message)` so the UI can show the engine's own validation
/// text (for example, a duplicate server name or a missing required field).
///
/// Secret values (env vars, headers) are never included in these errors and
/// never logged.
enum MCPServersAPIError: LocalizedError, Equatable, Sendable {
    case unreachable(String)
    case server(Int, String)
    case decoding(String)

    var errorDescription: String? {
        switch self {
        case .unreachable(let detail):
            return "Can't reach the connector's local management API (\(detail))."
        case .server(let code, let message):
            return message.isEmpty ? "The management API returned HTTP \(code)." : message
        case .decoding(let detail):
            return "Could not read the management API response: \(detail)"
        }
    }
}

// MARK: - Transport

/// How the connector reaches a hosted MCP server. Raw values match the engine's
/// JSON/YAML exactly (`stdio`, `http`, `sse`).
enum MCPTransport: String, Codable, CaseIterable, Identifiable, Sendable {
    case stdio
    case http
    case sse

    var id: String { rawValue }

    var displayName: String {
        switch self {
        case .stdio: return "stdio"
        case .http:  return "http"
        case .sse:   return "sse"
        }
    }

    /// stdio launches a local subprocess (command/args/env); the network
    /// transports use url/headers.
    var usesCommand: Bool { self == .stdio }
}

// MARK: - Models

/// One row of `GET /api/mcp-servers` (and the `servers` array of
/// `GET /api/status`): the configured server plus its live status summary.
struct HostedMCPServer: Codable, Equatable, Identifiable, Sendable {
    var name: String
    var transport: MCPTransport
    var enabled: Bool
    var connected: Bool
    var error: String?
    var toolCount: Int

    var id: String { name }

    init(name: String,
         transport: MCPTransport,
         enabled: Bool = true,
         connected: Bool = false,
         error: String? = nil,
         toolCount: Int = 0) {
        self.name = name
        self.transport = transport
        self.enabled = enabled
        self.connected = connected
        self.error = error
        self.toolCount = toolCount
    }
}

/// One tool discovered on a hosted server (the items of
/// `GET /api/mcp-servers/{name}/tools`). `name` is the server-local namespaced
/// name (`<server>_<tool>`).
struct HostedMCPTool: Codable, Equatable, Identifiable, Sendable {
    var name: String
    var description: String
    var inputSchema: JSONValue?

    var id: String { name }
}

/// The live status block of one server (`status` on the detail response).
struct HostedMCPServerStatus: Codable, Equatable, Sendable {
    var connected: Bool
    var error: String?
    var toolCount: Int
    var tools: [HostedMCPTool]

    init(connected: Bool = false,
         error: String? = nil,
         toolCount: Int = 0,
         tools: [HostedMCPTool] = []) {
        self.connected = connected
        self.error = error
        self.toolCount = toolCount
        self.tools = tools
    }
}

/// Request payload for creating or replacing a server (`POST /api/mcp-servers`,
/// `PUT /api/mcp-servers/{name}/config`). Field names match the engine's
/// `serverRequest` exactly, including the snake_case `disabled_tools`.
///
/// The same shape is decoded from `GET /api/mcp-servers/{name}` (minus the
/// nested `status` block, which `HostedMCPServerDetail` carries separately).
struct HostedMCPServerConfig: Codable, Equatable, Sendable {
    var name: String
    var transport: MCPTransport
    var enabled: Bool
    var command: String?
    var args: [String]?
    var env: [String: String]?
    var url: String?
    var headers: [String: String]?
    var disabledTools: [String]?

    enum CodingKeys: String, CodingKey {
        case name, transport, enabled, command, args, env, url, headers
        case disabledTools = "disabled_tools"
    }

    init(name: String,
         transport: MCPTransport,
         enabled: Bool = true,
         command: String? = nil,
         args: [String]? = nil,
         env: [String: String]? = nil,
         url: String? = nil,
         headers: [String: String]? = nil,
         disabledTools: [String]? = nil) {
        self.name = name
        self.transport = transport
        self.enabled = enabled
        self.command = command
        self.args = args
        self.env = env
        self.url = url
        self.headers = headers
        self.disabledTools = disabledTools
    }
}

/// Full config + live status returned by `GET /api/mcp-servers/{name}` and by
/// the create/update/toggle endpoints. The flat `serverDetail` JSON is split
/// into `config` (name/transport/connection fields) and `status`.
struct HostedMCPServerDetail: Decodable, Equatable, Sendable {
    var config: HostedMCPServerConfig
    var status: HostedMCPServerStatus

    enum CodingKeys: String, CodingKey { case status }

    init(config: HostedMCPServerConfig, status: HostedMCPServerStatus) {
        self.config = config
        self.status = status
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        config = try HostedMCPServerConfig(from: decoder)
        status = try container.decodeIfPresent(HostedMCPServerStatus.self, forKey: .status)
            ?? HostedMCPServerStatus()
    }
}

/// `GET /api/status` — the relay's own runtime state plus per-server summaries.
struct RelayRuntimeStatus: Codable, Equatable, Sendable {
    var instanceID: String
    var serverURL: String
    var connected: Bool
    var toolCount: Int
    var servers: [HostedMCPServer]
}

// MARK: - JSON value

/// A decoded arbitrary JSON value, used for a tool's free-form `inputSchema`.
/// Presented only for completeness; the UI renders the tool's name and
/// description.
enum JSONValue: Codable, Equatable, Sendable {
    case string(String)
    case number(Double)
    case bool(Bool)
    case null
    case array([JSONValue])
    case object([String: JSONValue])

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
        } else if let value = try? container.decode(Bool.self) {
            self = .bool(value)
        } else if let value = try? container.decode(Double.self) {
            self = .number(value)
        } else if let value = try? container.decode(String.self) {
            self = .string(value)
        } else if let value = try? container.decode([JSONValue].self) {
            self = .array(value)
        } else if let value = try? container.decode([String: JSONValue].self) {
            self = .object(value)
        } else {
            throw DecodingError.dataCorruptedError(in: container,
                                                   debugDescription: "Unsupported JSON value")
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .string(let value): try container.encode(value)
        case .number(let value): try container.encode(value)
        case .bool(let value): try container.encode(value)
        case .null: try container.encodeNil()
        case .array(let value): try container.encode(value)
        case .object(let value): try container.encode(value)
        }
    }
}

// MARK: - Client

/// Thin `URLSession` client for the connector's loopback management API
/// (default `http://127.0.0.1:8931`).
///
/// The API is bound to 127.0.0.1 and unauthenticated by design (local-companion
/// trust model), so this client sends no credentials. `baseURL` and `session`
/// are injectable so tests can stub the network.
struct MCPServersAPIClient: Sendable {
    /// Default loopback base URL; the engine serves the API on `--api-port 8931`
    /// (not 8890, which the Diane companion uses on the same host).
    static let defaultBaseURL = "http://127.0.0.1:8931"

    let baseURL: String
    let session: URLSession

    init(baseURL: String = MCPServersAPIClient.defaultBaseURL, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.session = session
    }

    // MARK: Endpoints

    /// `GET /api/mcp-servers` — configured servers with live status summaries.
    func list() async throws -> [HostedMCPServer] {
        try await get("/api/mcp-servers", as: [HostedMCPServer].self)
    }

    /// `GET /api/mcp-servers/{name}` — full config + live status.
    func get(name: String) async throws -> HostedMCPServerDetail {
        try await get("/api/mcp-servers/\(Self.pathEscape(name))", as: HostedMCPServerDetail.self)
    }

    /// `POST /api/mcp-servers` — create a server (HTTP 201 + detail).
    func create(_ config: HostedMCPServerConfig) async throws -> HostedMCPServerDetail {
        let data = try await perform(method: "POST", path: "/api/mcp-servers",
                                     body: try JSONEncoder().encode(config))
        return try decode(HostedMCPServerDetail.self, from: data)
    }

    /// `PUT /api/mcp-servers/{name}/config` — replace a server's config.
    func update(name: String, config: HostedMCPServerConfig) async throws -> HostedMCPServerDetail {
        let data = try await perform(method: "PUT",
                                     path: "/api/mcp-servers/\(Self.pathEscape(name))/config",
                                     body: try JSONEncoder().encode(config))
        return try decode(HostedMCPServerDetail.self, from: data)
    }

    /// `PUT /api/mcp-servers/{name}/enabled` — enable/disable without touching
    /// the rest of the config.
    func setEnabled(name: String, enabled: Bool) async throws -> HostedMCPServerDetail {
        struct Body: Encodable { let enabled: Bool }
        let data = try await perform(method: "PUT",
                                     path: "/api/mcp-servers/\(Self.pathEscape(name))/enabled",
                                     body: try JSONEncoder().encode(Body(enabled: enabled)))
        return try decode(HostedMCPServerDetail.self, from: data)
    }

    /// `DELETE /api/mcp-servers/{name}` — removes the server (HTTP 204).
    func delete(name: String) async throws {
        _ = try await perform(method: "DELETE",
                              path: "/api/mcp-servers/\(Self.pathEscape(name))")
    }

    /// `GET /api/mcp-servers/{name}/tools` — tools discovered on a server.
    func tools(name: String) async throws -> [HostedMCPTool] {
        try await get("/api/mcp-servers/\(Self.pathEscape(name))/tools", as: [HostedMCPTool].self)
    }

    /// `GET /api/status` — relay runtime state.
    func status() async throws -> RelayRuntimeStatus {
        try await get("/api/status", as: RelayRuntimeStatus.self)
    }

    // MARK: Plumbing

    private func get<T: Decodable>(_ path: String, as type: T.Type) async throws -> T {
        let data = try await perform(method: "GET", path: path)
        return try decode(type, from: data)
    }

    private func decode<T: Decodable>(_ type: T.Type, from data: Data) throws -> T {
        do {
            return try JSONDecoder().decode(type, from: data)
        } catch {
            throw MCPServersAPIError.decoding(error.localizedDescription)
        }
    }

    /// Builds and runs one request, mapping transport/status failures to
    /// `MCPServersAPIError`. Returns the raw body on any 2xx.
    private func perform(method: String, path: String, body: Data? = nil) async throws -> Data {
        var trimmedBase = baseURL.trimmingCharacters(in: .whitespacesAndNewlines)
        while trimmedBase.hasSuffix("/") { trimmedBase.removeLast() }
        guard let url = URL(string: trimmedBase + path) else {
            throw MCPServersAPIError.unreachable("invalid base URL")
        }

        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if let body {
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch {
            throw MCPServersAPIError.unreachable(error.localizedDescription)
        }

        guard let http = response as? HTTPURLResponse else {
            throw MCPServersAPIError.unreachable("non-HTTP response")
        }
        guard (200..<300).contains(http.statusCode) else {
            throw MCPServersAPIError.server(http.statusCode, Self.errorMessage(from: data))
        }
        return data
    }

    /// Extracts the `{"error": "..."}` message the API returns for failures.
    private static func errorMessage(from data: Data) -> String {
        struct ErrorBody: Decodable { let error: String? }
        return (try? JSONDecoder().decode(ErrorBody.self, from: data))?.error ?? ""
    }

    /// Percent-encodes a server name for a path component (names are usually
    /// slugs, but a space or slash must not break the URL).
    private static func pathEscape(_ name: String) -> String {
        var allowed = CharacterSet.urlPathAllowed
        allowed.remove(charactersIn: "/?#%")
        return name.addingPercentEncoding(withAllowedCharacters: allowed) ?? name
    }
}
