import Foundation

/// Thin `URLSession` client for the connector's loopback registry endpoints
/// (`GET /api/mcp-registry/*`), which proxy the official MCP registry through
/// Memory.
///
/// Shares the loopback base URL and the `MCPServersAPIError` mapping with
/// ``MCPServersAPIClient``; kept separate so the registry surface and its models
/// stay self-contained. The API is bound to 127.0.0.1 and unauthenticated by
/// design (local-companion trust model), so this client sends no credentials.
/// `baseURL` and `session` are injectable so tests can stub the network.
struct MCPRegistryAPIClient: Sendable {
    /// Default loopback base URL; the engine serves the management API on
    /// `--api-port 8931` (see ``MCPServersAPIClient/defaultBaseURL``). Additional
    /// connected projects serve on probed ports, so callers project-scope their
    /// client via `baseURL(port:)`.
    static let defaultBaseURL = baseURL(port: 8931)

    /// The loopback base URL for a management port.
    static func baseURL(port: Int) -> String {
        MCPServersAPIClient.baseURL(port: port)
    }

    let baseURL: String
    let session: URLSession

    init(baseURL: String = MCPRegistryAPIClient.defaultBaseURL, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.session = session
    }

    // MARK: Endpoints

    /// `GET /api/mcp-registry/search?q=&limit=&cursor=` — official-registry
    /// entries proxied through Memory. Query parameters are percent-encoded; an
    /// empty `q` lists entries.
    func search(query: String,
                limit: Int? = nil,
                cursor: String? = nil) async throws -> MCPRegistrySearchResult
    {
        var items = [URLQueryItem(name: "q", value: query)]
        if let limit {
            items.append(URLQueryItem(name: "limit", value: String(limit)))
        }
        if let cursor, !cursor.isEmpty {
            items.append(URLQueryItem(name: "cursor", value: cursor))
        }
        var components = URLComponents()
        components.queryItems = items
        let encodedQuery = components.percentEncodedQuery ?? ""
        let path = encodedQuery.isEmpty
            ? "/api/mcp-registry/search"
            : "/api/mcp-registry/search?\(encodedQuery)"
        return try await get(path, as: MCPRegistrySearchResult.self)
    }

    /// `GET /api/mcp-registry/servers/{name}` — one entry plus the connector's
    /// suggested local config. Registry ids contain `/`, so the name is escaped
    /// so it stays a single path component.
    func server(name: String) async throws -> MCPRegistryServerDetail {
        try await get("/api/mcp-registry/servers/\(Self.pathEscape(name))",
                      as: MCPRegistryServerDetail.self)
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

    /// Percent-encodes a registry id for a path component. Registry ids contain
    /// `/`, which must stay a single path segment.
    private static func pathEscape(_ name: String) -> String {
        var allowed = CharacterSet.urlPathAllowed
        allowed.remove(charactersIn: "/?#%")
        return name.addingPercentEncoding(withAllowedCharacters: allowed) ?? name
    }
}
