import Foundation

/// Fetching seam for the object card, so ``ObjectCardStore`` is testable
/// without a network. ``ObjectCardClient`` is the live implementation.
protocol ObjectCardFetching: Sendable {
    func card(for reference: ObjectReference) async throws -> ObjectCard
    func history(for reference: ObjectReference) async throws -> [ObjectCardVersion]
}

/// Talks to the gateway's device object-card routes:
///
/// - `GET /api/objects/{ref}/card[?rel=<relationship-id>]` → ``ObjectCard``
/// - `GET /api/objects/{ref}/history` → `{"versions": [ObjectCardVersion]}`
///
/// The base is ``MemoryConfig/gatewayBaseURL``, the gateway the chat stream
/// talks to, because a card is opened from that chat's content.
struct ObjectCardClient: ObjectCardFetching {
    let config: MemoryConfig
    var session: URLSession = .shared

    func card(for reference: ObjectReference) async throws -> ObjectCard {
        var query: [URLQueryItem] = []
        if let rel = reference.relationshipID {
            query.append(URLQueryItem(name: "rel", value: rel))
        }
        let data = try await send(makeURL(reference, tail: "card", query: query))
        do {
            return try JSONDecoder().decode(ObjectCard.self, from: data)
        } catch {
            throw GatewayError.decode("Gateway returned an unexpected object card: \(error.localizedDescription)")
        }
    }

    func history(for reference: ObjectReference) async throws -> [ObjectCardVersion] {
        let data = try await send(makeURL(reference, tail: "history", query: []))
        do {
            return try JSONDecoder().decode(HistoryResponse.self, from: data).versions
        } catch {
            throw GatewayError.decode("Gateway returned an unexpected object history: \(error.localizedDescription)")
        }
    }

    /// `<gateway>/api/objects/{ref}/{tail}?…`. `appending(path:)` percent-encodes
    /// the ref as one path segment.
    func makeURL(_ reference: ObjectReference, tail: String, query: [URLQueryItem]) throws -> URL {
        guard let base = config.gatewayBaseURL else {
            throw GatewayError.invalidConfiguration(
                "Could not derive the gateway URL from the token endpoint: \(config.tokenEndpoint)"
            )
        }
        let url = base
            .appending(path: "api")
            .appending(path: "objects")
            .appending(path: reference.ref)
            .appending(path: tail)
        guard !query.isEmpty else { return url }
        guard var components = URLComponents(url: url, resolvingAgainstBaseURL: false) else {
            throw GatewayError.invalidConfiguration("Could not build the object card URL.")
        }
        components.queryItems = query
        guard let finalURL = components.url else {
            throw GatewayError.invalidConfiguration("Could not build the object card URL.")
        }
        return finalURL
    }

    private var service: GatewayServiceMessages {
        GatewayServiceMessages(
            httpPrefix: "Object card",
            noResponsePrefix: "Gateway",
            reachTarget: "the gateway at \(config.gatewayBaseURL?.absoluteString ?? config.tokenEndpoint)"
        )
    }

    private func send(_ url: URL) async throws -> Data {
        let request = GatewayHTTP.makeRequest(method: "GET", url: url, apiKey: config.apiKey)
        return try await GatewayHTTP.sendChecked(request, service: service, session: session)
    }
}

/// `{"versions": [...]}`; a missing list is empty.
private struct HistoryResponse: Decodable {
    let versions: [ObjectCardVersion]

    private enum CodingKeys: String, CodingKey {
        case versions
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        versions = (try? container.decodeIfPresent([ObjectCardVersion].self, forKey: .versions)) ?? []
    }
}
