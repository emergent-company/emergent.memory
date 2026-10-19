import Foundation
import LiveKit

/// Requests a room-join token from Memory's token endpoint.
///
/// The endpoint contract (see `agent/admin.py`, `_post_token`) is:
/// - `POST` with a JSON body `{ "identity": ..., "agent": ..., "client": ... }`
///   (the server owns room creation and assigns the room)
/// - an `X-API-Key` header (a scoped `emt_` device credential, checked server-side)
/// - a JSON response `{ "server_url": ..., "participant_token": ... }`
///   matching the SDK's ``TokenSourceResponse``.
///
/// The body shape differs from the SDK's default ``TokenRequestOptions``
/// encoding, so ``fetch(_:)`` is overridden to build the request explicitly.
/// The response is still decoded as ``TokenSourceResponse`` so the SDK's
/// `Session` flow is unchanged.
struct MemoryTokenSource: EndpointTokenSource {
    /// Request body sent to the token endpoint.
    private struct TokenRequestBody: Encodable {
        let identity: String
        let agent: String
        let client: String
    }

    let config: MemoryConfig

    /// The token endpoint URL (Memory's gateway), e.g.
    /// `http://localhost:8095/api/token`.
    ///
    /// Non-failable to satisfy the SDK's `EndpointTokenSource` contract; a
    /// malformed endpoint surfaces as a clean error from `makeRequest`.
    var url: URL {
        URL(string: config.tokenEndpoint) ?? URL(fileURLWithPath: "/invalid-token-endpoint")
    }

    /// The `X-API-Key` header is required by the token endpoint.
    var headers: [String: String] {
        ["X-API-Key": config.apiKey]
    }

    func fetch(_ options: TokenRequestOptions) async throws -> TokenSourceResponse {
        TraceLog.log("token_fetch_start")
        do {
            let request = try makeRequest(for: options)
            let data = try await send(request)
            let response = try decode(data)
            TraceLog.log("token_fetched", ["agent": options.agentName ?? config.agentName])
            return response
        } catch {
            TraceLog.log("token_fetch_error", ["message": error.localizedDescription])
            throw error
        }
    }

    private func makeRequest(for options: TokenRequestOptions) throws -> URLRequest {
        guard let endpoint = URL(string: config.tokenEndpoint) else {
            throw GatewayError.transport(
                "Invalid token endpoint URL: \(config.tokenEndpoint)"
            )
        }
        var request = URLRequest(url: endpoint)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        for (key, value) in headers {
            request.setValue(value, forHTTPHeaderField: key)
        }
        let body = TokenRequestBody(
            identity: options.participantIdentity ?? config.participantIdentity,
            agent: options.agentName ?? config.agentName,
            client: "ios"
        )
        request.httpBody = try JSONEncoder().encode(body)
        return request
    }

    private var service: GatewayServiceMessages {
        GatewayServiceMessages(
            httpPrefix: "Token endpoint",
            noResponsePrefix: "Token endpoint",
            reachTarget: "the token endpoint at \(config.tokenEndpoint)"
        )
    }

    private func send(_ request: URLRequest) async throws -> Data {
        try await GatewayHTTP.sendChecked(request, service: service)
    }

    private func decode(_ data: Data) throws -> TokenSourceResponse {
        let tokenResponse: TokenSourceResponse
        do {
            tokenResponse = try JSONDecoder().decode(TokenSourceResponse.self, from: data)
        } catch {
            throw GatewayError.decode(
                "Token endpoint returned an unexpected response: \(error.localizedDescription)"
            )
        }

        // The token server is authoritative for the WebSocket URL, but fall
        // back to the configured server URL if it returns an empty one.
        if tokenResponse.serverURL.absoluteString.isEmpty {
            guard let fallbackURL = URL(string: config.serverURL) else {
                throw GatewayError.decode(
                    "Token endpoint returned an empty server URL and the configured server URL is invalid: \(config.serverURL)"
                )
            }
            return TokenSourceResponse(
                serverURL: fallbackURL,
                participantToken: tokenResponse.participantToken,
                participantName: tokenResponse.participantName,
                roomName: tokenResponse.roomName
            )
        }
        return tokenResponse
    }
}
