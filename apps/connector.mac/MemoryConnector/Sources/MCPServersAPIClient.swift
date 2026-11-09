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
        case let .unreachable(detail):
            "Can't reach the connector's local management API (\(detail))."
        case let .server(code, message):
            message.isEmpty ? "The management API returned HTTP \(code)." : message
        case let .decoding(detail):
            "Could not read the management API response: \(detail)"
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

    var id: String {
        rawValue
    }

    var displayName: String {
        switch self {
        case .stdio: "stdio"
        case .http: "http"
        case .sse: "sse"
        }
    }

    /// stdio launches a local subprocess (command/args/env); the network
    /// transports use url/headers.
    var usesCommand: Bool {
        self == .stdio
    }
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
    /// True when Memory manages this server. Managed servers are configured in
    /// Memory and are read-only locally. Absent from older connector API
    /// responses, so it decodes as `false` when missing.
    var managed: Bool

    var id: String {
        name
    }

    enum CodingKeys: String, CodingKey {
        case name, transport, enabled, connected, error, toolCount, managed
    }

    init(name: String,
         transport: MCPTransport,
         enabled: Bool = true,
         connected: Bool = false,
         error: String? = nil,
         toolCount: Int = 0,
         managed: Bool = false)
    {
        self.name = name
        self.transport = transport
        self.enabled = enabled
        self.connected = connected
        self.error = error
        self.toolCount = toolCount
        self.managed = managed
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decode(String.self, forKey: .name)
        transport = try container.decode(MCPTransport.self, forKey: .transport)
        enabled = try container.decode(Bool.self, forKey: .enabled)
        connected = try container.decode(Bool.self, forKey: .connected)
        error = try container.decodeIfPresent(String.self, forKey: .error)
        toolCount = try container.decode(Int.self, forKey: .toolCount)
        managed = try container.decodeIfPresent(Bool.self, forKey: .managed) ?? false
    }
}

/// One tool discovered on a hosted server (the items of
/// `GET /api/mcp-servers/{name}/tools`). `name` is the server-local namespaced
/// name (`<server>_<tool>`).
struct HostedMCPTool: Codable, Equatable, Identifiable, Sendable {
    var name: String
    var description: String
    var inputSchema: JSONValue?

    var id: String {
        name
    }
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
         tools: [HostedMCPTool] = [])
    {
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
         disabledTools: [String]? = nil)
    {
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

/// One entry the connector skipped during a Claude-config import.
struct HostedMCPImportSkip: Decodable, Equatable, Sendable {
    var name: String
    var reason: String
}

/// One entry that failed to import.
struct HostedMCPImportFailure: Decodable, Equatable, Sendable {
    var name: String
    var error: String
}

/// `POST /api/mcp-servers/import` result. Valid entries are applied even when
/// others fail.
struct HostedMCPImportResult: Decodable, Equatable, Sendable {
    var created: [String]
    var updated: [String]
    var skipped: [HostedMCPImportSkip]
    var failed: [HostedMCPImportFailure]
    var dryRun: Bool
}

/// Full config + live status returned by `GET /api/mcp-servers/{name}` and by
/// the create/update/toggle endpoints. The flat `serverDetail` JSON is split
/// into `config` (name/transport/connection fields) and `status`.
struct HostedMCPServerDetail: Decodable, Equatable, Sendable {
    var config: HostedMCPServerConfig
    var status: HostedMCPServerStatus
    /// True when Memory manages this server (read-only locally). Absent from
    /// older connector API responses, so it decodes as `false` when missing.
    var managed: Bool

    enum CodingKeys: String, CodingKey { case status, managed }

    init(config: HostedMCPServerConfig, status: HostedMCPServerStatus, managed: Bool = false) {
        self.config = config
        self.status = status
        self.managed = managed
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        config = try HostedMCPServerConfig(from: decoder)
        status = try container.decodeIfPresent(HostedMCPServerStatus.self, forKey: .status)
            ?? HostedMCPServerStatus()
        managed = try container.decodeIfPresent(Bool.self, forKey: .managed) ?? false
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

// MARK: - Client

/// Thin `URLSession` client for the connector's loopback management API
/// (default `http://127.0.0.1:8931`).
///
/// The API is bound to 127.0.0.1 and unauthenticated by design (local-companion
/// trust model), so this client sends no credentials. `baseURL` and `session`
/// are injectable so tests can stub the network.
struct MCPServersAPIClient: Sendable {
    /// Default loopback base URL; the engine serves the API on `--api-port 8931`
    /// (not 8890, which the Diane companion uses on the same host). Additional
    /// connected projects serve on probed ports, so callers project-scope their
    /// client via `baseURL(port:)`.
    static let defaultBaseURL = baseURL(port: 8931)

    /// The loopback base URL for a management port.
    static func baseURL(port: Int) -> String {
        "http://127.0.0.1:\(port)"
    }

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
                                     body: JSONEncoder().encode(config))
        return try decode(HostedMCPServerDetail.self, from: data)
    }

    /// `PUT /api/mcp-servers/{name}/config` — replace a server's config.
    func update(name: String, config: HostedMCPServerConfig) async throws -> HostedMCPServerDetail {
        let data = try await perform(method: "PUT",
                                     path: "/api/mcp-servers/\(Self.pathEscape(name))/config",
                                     body: JSONEncoder().encode(config))
        return try decode(HostedMCPServerDetail.self, from: data)
    }

    /// `PUT /api/mcp-servers/{name}/enabled` — enable/disable without touching
    /// the rest of the config.
    func setEnabled(name: String, enabled: Bool) async throws -> HostedMCPServerDetail {
        struct Body: Encodable { let enabled: Bool }
        let data = try await perform(method: "PUT",
                                     path: "/api/mcp-servers/\(Self.pathEscape(name))/enabled",
                                     body: JSONEncoder().encode(Body(enabled: enabled)))
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

    /// `POST /api/mcp-servers/import` — import a Claude-format config document.
    /// The document is parsed and embedded as a JSON object (not a string), so
    /// the connector receives the same shape Claude writes.
    func importClaudeConfig(config: Data, overwrite: Bool, dryRun: Bool) async throws -> HostedMCPImportResult {
        let object: Any
        do {
            object = try JSONSerialization.jsonObject(with: config)
        } catch {
            throw MCPServersAPIError.decoding("The config is not valid JSON.")
        }
        let payload: [String: Any] = [
            "config": object,
            "overwrite": overwrite,
            "dryRun": dryRun,
        ]
        let body: Data
        do {
            body = try JSONSerialization.data(withJSONObject: payload)
        } catch {
            throw MCPServersAPIError.decoding(error.localizedDescription)
        }
        let data = try await perform(method: "POST", path: "/api/mcp-servers/import", body: body)
        return try decode(HostedMCPImportResult.self, from: data)
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
        while trimmedBase.hasSuffix("/") {
            trimmedBase.removeLast()
        }
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
        guard (200 ..< 300).contains(http.statusCode) else {
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
