import Foundation
import LiveKit

/// Errors thrown by ``MemoryTokenSource`` while talking to Memory's token
/// endpoint (`POST /api/token`).
enum MemoryTokenError: LocalizedError, Sendable {
    /// The endpoint answered with a non-2xx status code.
    case httpStatus(code: Int, message: String)
    /// The endpoint could not be reached at all (DNS/TCP/TLS failure).
    case transport(message: String)
    /// The endpoint answered but the body could not be decoded.
    case decode(message: String)

    var errorDescription: String? {
        switch self {
        case let .httpStatus(code, message):
            "Token endpoint returned HTTP \(code): \(message)"
        case let .transport(message):
            message
        case let .decode(message):
            message
        }
    }
}

/// Requests a room-join token from Memory's token endpoint.
///
/// The endpoint contract (see `agent/admin.py`, `_post_token`) is:
/// - `POST` with a JSON body `{ "identity": ..., "agent": ..., "client": ... }`
///   (the server owns room creation and assigns the room)
/// - an `X-API-Key` header (shared secret, checked server-side)
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

    /// Error body returned by the token endpoint on failure.
    private struct ErrorResponse: Decodable {
        let error: String
    }

    let config: MemoryConfig

    /// The token endpoint URL (Memory's admin server), e.g.
    /// `http://100.69.175.118:8080/api/token`.
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
            throw MemoryTokenError.transport(
                message: "Invalid token endpoint URL: \(config.tokenEndpoint)"
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

    private func send(_ request: URLRequest) async throws -> Data {
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await URLSession.shared.data(for: request)
        } catch {
            Log.net.error("transport \(request.url?.absoluteString ?? "unknown"): \(error.localizedDescription)")
            throw MemoryTokenError.transport(
                message: "Could not reach the token endpoint at \(config.tokenEndpoint): \(error.localizedDescription)"
            )
        }

        guard let httpResponse = response as? HTTPURLResponse else {
            throw MemoryTokenError.transport(message: "Token endpoint returned no HTTP response.")
        }
        guard (200 ..< 300).contains(httpResponse.statusCode) else {
            let serverMessage = (try? JSONDecoder().decode(ErrorResponse.self, from: data))?.error
                ?? HTTPURLResponse.localizedString(forStatusCode: httpResponse.statusCode)
            Log.net.error("HTTP \(httpResponse.statusCode) \(request.url?.absoluteString ?? "unknown"): \(serverMessage)")
            throw MemoryTokenError.httpStatus(code: httpResponse.statusCode, message: serverMessage)
        }
        return data
    }

    private func decode(_ data: Data) throws -> TokenSourceResponse {
        let tokenResponse: TokenSourceResponse
        do {
            tokenResponse = try JSONDecoder().decode(TokenSourceResponse.self, from: data)
        } catch {
            throw MemoryTokenError.decode(
                message: "Token endpoint returned an unexpected response: \(error.localizedDescription)"
            )
        }

        // The token server is authoritative for the WebSocket URL, but fall
        // back to the configured server URL if it returns an empty one.
        if tokenResponse.serverURL.absoluteString.isEmpty {
            guard let fallbackURL = URL(string: config.serverURL) else {
                throw MemoryTokenError.decode(
                    message: "Token endpoint returned an empty server URL and the configured server URL is invalid: \(config.serverURL)"
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
