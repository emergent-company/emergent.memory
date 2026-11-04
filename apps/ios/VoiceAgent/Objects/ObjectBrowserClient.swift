import Foundation

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
/// The gateway lives at `config.apiBaseURL` and uses the same `X-API-Key` header
/// (see `ControlPlaneClient`). The base URL is `config.apiBaseURL` directly —
/// not derived from the token endpoint.
///
/// Endpoint:
/// - `GET /api/agents/{id}/objects?provenance=&cursor=` →
///   `{"items": [GraphObject], "next_cursor": "..."}`
struct ObjectBrowserClient: ObjectFetching {
    let config: MemoryConfig

    /// The gateway base URL from `config.apiBaseURL`.
    var gatewayBaseURL: URL? {
        URL(string: config.apiBaseURL)
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
            throw GatewayError.decode(
                "Gateway returned an unexpected object list: \(error.localizedDescription)"
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
            throw GatewayError.invalidConfiguration(
                "Could not build the object-browser URL from apiBaseURL: \(config.apiBaseURL)"
            )
        }
        let url = base
            .appending(path: "api")
            .appending(path: "agents")
            .appending(path: agentID)
            .appending(path: "objects")
        guard var components = URLComponents(url: url, resolvingAgainstBaseURL: false) else {
            throw GatewayError.invalidConfiguration("Could not build the object browser URL.")
        }
        var items = [URLQueryItem(name: "provenance", value: provenance.rawValue)]
        if let cursor, !cursor.isEmpty {
            items.append(URLQueryItem(name: "cursor", value: cursor))
        }
        components.queryItems = items
        guard let finalURL = components.url else {
            throw GatewayError.invalidConfiguration("Could not build the object browser URL.")
        }
        return finalURL
    }

    // MARK: - Transport

    /// The `X-API-Key` header is required by the gateway, same as the token
    /// endpoint (sent by ``GatewayHTTP``).
    private var service: GatewayServiceMessages {
        GatewayServiceMessages(
            httpPrefix: "Object browser",
            noResponsePrefix: "Gateway",
            reachTarget: "the gateway at \(config.apiBaseURL)"
        )
    }

    private func send(url: URL) async throws -> Data {
        let request = GatewayHTTP.makeRequest(method: "GET", url: url, apiKey: config.apiKey)
        return try await GatewayHTTP.sendChecked(request, service: service)
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
