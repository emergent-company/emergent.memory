import Foundation

/// Errors thrown by ``ObjectBrowserClient`` while talking to the gateway.
enum ObjectBrowserError: LocalizedError, Sendable {
    /// The endpoint answered with a non-2xx status code (401 auth, 404 unknown
    /// agent, 502 backend unavailable, ...).
    case httpStatus(code: Int, message: String)
    /// The endpoint could not be reached at all (DNS/TCP/TLS failure).
    case transport(message: String)
    /// The endpoint answered but the body could not be decoded.
    case decode(message: String)
    /// The configured token endpoint cannot be turned into a gateway URL.
    case invalidConfiguration(message: String)

    var errorDescription: String? {
        switch self {
        case let .httpStatus(code, message):
            "Object browser returned HTTP \(code): \(message)"
        case let .transport(message):
            message
        case let .decode(message):
            message
        case let .invalidConfiguration(message):
            message
        }
    }
}

/// Provenance filter for the object browser. Raw values match the gateway's
/// `provenance` query param: `any` (created or updated), `created`, `updated`.
enum ObjectProvenance: String, CaseIterable, Sendable {
    case any
    case created
    case updated
}

/// One page of objects plus the cursor for the next page (nil when exhausted).
struct ObjectPage: Equatable, Sendable {
    let items: [GraphObject]
    let nextCursor: String?
}

/// Fetching seam for the object browser, so ``ObjectBrowserStore`` is testable
/// without a network. ``ObjectBrowserClient`` is the live implementation.
protocol ObjectFetching: Sendable {
    func listObjects(
        agentID: String,
        provenance: ObjectProvenance,
        cursor: String?
    ) async throws -> ObjectPage
}

/// Talks to the gateway's device object-browser endpoint.
///
/// The gateway lives on the same host:port as the token endpoint and uses the
/// same `X-API-Key` header. The base URL is derived from
/// `config.tokenEndpoint` by stripping the `/api/token` path suffix, e.g.
/// `http://host:8080/api/token` → `http://host:8080`.
///
/// Endpoint:
/// - `GET /api/agents/{id}/objects?provenance=&cursor=` →
///   `{"items": [GraphObject], "next_cursor": "..."}`
struct ObjectBrowserClient: ObjectFetching {
    let config: MemoryConfig

    /// The gateway base URL, derived from the token endpoint by stripping the
    /// `/api/token` suffix.
    var gatewayBaseURL: URL? {
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

    /// Fetches one page of `agentID`'s objects, filtered by `provenance`.
    func listObjects(
        agentID: String,
        provenance: ObjectProvenance,
        cursor: String? = nil
    ) async throws -> ObjectPage {
        let url = try makeURL(agentID: agentID, provenance: provenance, cursor: cursor)
        let data = try await send(url: url)
        return try Self.decodePage(data)
    }

    /// Decodes the `{items, next_cursor}` page shape. An empty cursor decodes
    /// to nil (no more pages).
    static func decodePage(_ data: Data) throws -> ObjectPage {
        let response: ObjectsResponse
        do {
            response = try JSONDecoder().decode(ObjectsResponse.self, from: data)
        } catch {
            throw ObjectBrowserError.decode(
                message: "Gateway returned an unexpected object list: \(error.localizedDescription)"
            )
        }
        let cursor = response.nextCursor.flatMap { $0.isEmpty ? nil : $0 }
        return ObjectPage(items: response.items, nextCursor: cursor)
    }

    // MARK: - Request building

    /// Builds `<gatewayBaseURL>/api/agents/{id}/objects?provenance=&cursor=`.
    /// `provenance` is always sent; `cursor` only when non-empty.
    func makeURL(agentID: String, provenance: ObjectProvenance, cursor: String?) throws -> URL {
        guard let base = gatewayBaseURL else {
            throw ObjectBrowserError.invalidConfiguration(
                message: "Could not derive the gateway URL from the token endpoint: \(config.tokenEndpoint)"
            )
        }
        let url = base
            .appending(path: "api")
            .appending(path: "agents")
            .appending(path: agentID)
            .appending(path: "objects")
        guard var components = URLComponents(url: url, resolvingAgainstBaseURL: false) else {
            throw ObjectBrowserError.invalidConfiguration(message: "Could not build the object browser URL.")
        }
        var items = [URLQueryItem(name: "provenance", value: provenance.rawValue)]
        if let cursor, !cursor.isEmpty {
            items.append(URLQueryItem(name: "cursor", value: cursor))
        }
        components.queryItems = items
        guard let finalURL = components.url else {
            throw ObjectBrowserError.invalidConfiguration(message: "Could not build the object browser URL.")
        }
        return finalURL
    }

    // MARK: - Transport

    /// The `X-API-Key` header is required by the gateway, same as the token
    /// endpoint (sent by ``GatewayHTTP``).
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
                throw ObjectBrowserError.transport(
                    message: "Could not reach the gateway at \(config.apiBaseURL): \(underlying)"
                )
            case .noHTTPResponse:
                throw ObjectBrowserError.transport(message: "Gateway returned no HTTP response.")
            }
        }

        guard (200 ..< 300).contains(httpResponse.statusCode) else {
            let serverMessage = (try? JSONDecoder().decode(ErrorResponse.self, from: data))?.error
                ?? HTTPURLResponse.localizedString(forStatusCode: httpResponse.statusCode)
            Log.net.error("HTTP \(httpResponse.statusCode) \(url.absoluteString): \(serverMessage)")
            throw ObjectBrowserError.httpStatus(code: httpResponse.statusCode, message: serverMessage)
        }
        return data
    }

    // MARK: - Response models

    /// Error body returned by the gateway on failure.
    private struct ErrorResponse: Decodable {
        let error: String
    }
}

/// One `{items, next_cursor}` page from the gateway. Decoded defensively: a
/// missing `items` is an empty list and an absent cursor means no more pages.
private struct ObjectsResponse: Decodable {
    let items: [GraphObject]
    let nextCursor: String?

    private enum CodingKeys: String, CodingKey {
        case items
        case nextCursor = "next_cursor"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        items = (try? container.decodeIfPresent([GraphObject].self, forKey: .items)) ?? []
        nextCursor = (try? container.decodeIfPresent(String.self, forKey: .nextCursor)) ?? nil
    }
}
