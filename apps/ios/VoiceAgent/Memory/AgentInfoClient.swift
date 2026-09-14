import Foundation

/// Errors thrown by ``AgentInfoClient`` while talking to the memory service.
enum AgentInfoError: LocalizedError, Sendable {
    /// The endpoint answered with a non-2xx status code (401 auth, 404
    /// unknown agent, 502 memory service unavailable, ...).
    case httpStatus(code: Int, message: String)
    /// The endpoint could not be reached at all (DNS/TCP/TLS failure).
    case transport(message: String)
    /// The endpoint answered but the body could not be decoded.
    case decode(message: String)
    /// The configured token endpoint cannot be turned into a memories URL.
    case invalidConfiguration(message: String)

    var errorDescription: String? {
        switch self {
        case let .httpStatus(code, message):
            "Memory service returned HTTP \(code): \(message)"
        case let .transport(message):
            message
        case let .decode(message):
            message
        case let .invalidConfiguration(message):
            message
        }
    }
}

/// Talks to the memory service: capability check and memory list.
///
/// The memory service lives on the same host:port as the token endpoint and
/// uses the same `X-API-Key` header. The base URL is derived from
/// `config.tokenEndpoint` by stripping the `/api/token` path suffix, e.g.
/// `http://host:8080/api/token` → `http://host:8080/api/memories`.
///
/// Endpoints (see the memory service contract):
/// - `GET /api/memories/capability?agent=<name>` → `{"agent": ..., "hasMemory": bool}`
/// - `GET /api/memories?agent=<name>&query=<text>` (query optional) → `{"memories": [...]}`
struct AgentInfoClient: Sendable {
    let config: MemoryConfig

    /// The memory service base URL, derived from the token endpoint by
    /// stripping the `/api/token` suffix.
    var memoriesBaseURL: URL? {
        let endpoint = config.tokenEndpoint
        if endpoint.hasSuffix("/api/token") {
            return URL(string: String(endpoint.dropLast("/api/token".count)) + "/api/memories")
        }
        // Fallback for non-standard endpoints: treat the URL as a directory
        // (drops the last path component) and append `memories`.
        return URL(string: endpoint)?
            .deletingLastPathComponent()
            .appending(path: "memories")
    }

    /// Whether `agent` has memory (the agent worker stores memories).
    func fetchCapability(agent: String) async throws -> Bool {
        let url = try makeURL(subpath: "capability", agent: agent, query: nil)
        let data = try await send(request: URLRequest(url: url))
        let response: CapabilityResponse
        do {
            response = try JSONDecoder().decode(CapabilityResponse.self, from: data)
        } catch {
            throw AgentInfoError.decode(
                message: "Memory service returned an unexpected capability response: \(error.localizedDescription)"
            )
        }
        return response.hasMemory
    }

    /// Fetches memories for `agent`, optionally server-side searched by
    /// `query` (nil or empty fetches everything).
    func fetchMemories(agent: String, query: String? = nil) async throws -> [Memory] {
        let url = try makeURL(subpath: "", agent: agent, query: query)
        let data = try await send(request: URLRequest(url: url))
        let response: MemoriesResponse
        do {
            response = try JSONDecoder().decode(MemoriesResponse.self, from: data)
        } catch {
            throw AgentInfoError.decode(
                message: "Memory service returned an unexpected response: \(error.localizedDescription)"
            )
        }
        return response.memories
    }

    // MARK: - Request building

    /// Builds `GET <memoriesBaseURL>/<subpath>?agent=...&query=...`.
    private func makeURL(subpath: String, agent: String, query: String?) throws -> URL {
        guard let base = memoriesBaseURL else {
            throw AgentInfoError.invalidConfiguration(
                message: "Could not derive the memory service URL from the token endpoint: \(config.tokenEndpoint)"
            )
        }
        let url = subpath.isEmpty ? base : base.appending(path: subpath)
        var components = URLComponents(url: url, resolvingAgainstBaseURL: false)
        var items = [URLQueryItem(name: "agent", value: agent)]
        if let query, !query.isEmpty {
            items.append(URLQueryItem(name: "query", value: query))
        }
        components?.queryItems = items
        guard let finalURL = components?.url else {
            throw AgentInfoError.invalidConfiguration(message: "Could not build the memory service URL.")
        }
        return finalURL
    }

    // MARK: - Transport

    /// The `X-API-Key` header is required by the memory service, same as the
    /// token endpoint (sent by ``GatewayHTTP``).
    private func send(request: URLRequest) async throws -> Data {
        guard let url = request.url else {
            throw AgentInfoError.invalidConfiguration(message: "Could not build the memory service URL.")
        }

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
                throw AgentInfoError.transport(
                    message: "Could not reach the memory service at \(config.tokenEndpoint): \(underlying)"
                )
            case .noHTTPResponse:
                throw AgentInfoError.transport(message: "Memory service returned no HTTP response.")
            }
        }

        guard (200 ..< 300).contains(httpResponse.statusCode) else {
            let serverMessage = (try? JSONDecoder().decode(ErrorResponse.self, from: data))?.error
                ?? HTTPURLResponse.localizedString(forStatusCode: httpResponse.statusCode)
            throw AgentInfoError.httpStatus(code: httpResponse.statusCode, message: serverMessage)
        }
        return data
    }

    // MARK: - Response models

    private struct CapabilityResponse: Decodable {
        let agent: String
        let hasMemory: Bool
    }

    private struct MemoriesResponse: Decodable {
        let memories: [Memory]
    }

    /// Error body returned by the memory service on failure.
    private struct ErrorResponse: Decodable {
        let error: String
    }
}
