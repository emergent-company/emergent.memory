import Foundation

/// Errors thrown by ``ControlPlaneClient`` while talking to the control plane.
enum ControlPlaneError: LocalizedError, Sendable {
    /// The endpoint answered with a non-2xx status code (401 auth, 404
    /// unknown agent, 409 duplicate name, 502 service unavailable, ...).
    case httpStatus(code: Int, message: String)
    /// The endpoint could not be reached at all (DNS/TCP/TLS failure).
    case transport(message: String)
    /// The endpoint answered but the body could not be decoded.
    case decode(message: String)
    /// The configured `apiBaseURL` cannot be turned into a request URL.
    case invalidConfiguration(message: String)

    var errorDescription: String? {
        switch self {
        case let .httpStatus(code, message):
            "Control plane returned HTTP \(code): \(message)"
        case let .transport(message):
            message
        case let .decode(message):
            message
        case let .invalidConfiguration(message):
            message
        }
    }
}

/// Talks to the Memory gateway's Memory-backed agent API (`AgentDefinition`
/// CRUD, see `gateway/memory.go`).
///
/// The control plane lives on `config.apiBaseURL` (the gateway). Every request
/// sends the `X-API-Key` header.
///
/// Endpoints (the app is read-only — agent mutation lives on web/desktop):
/// - `GET    /api/agents`      → `[Agent]` (summaries)
///
/// Errors come back as `{"error": "..."}` (409 → duplicate name).
struct ControlPlaneClient: Sendable {
    let config: MemoryConfig

    /// The control-plane base URL from `config.apiBaseURL`.
    var baseURL: URL? {
        URL(string: config.apiBaseURL)
    }

    // MARK: - Agents

    /// Fetches all configured agents (summary shape).
    func listAgents() async throws -> [Agent] {
        let url = try makeURL(path: "api/agents")
        let data = try await send(method: "GET", url: url, body: nil)
        do {
            return try JSONDecoder().decode([Agent].self, from: data)
        } catch {
            throw ControlPlaneError.decode(
                message: "Control plane returned an unexpected agent list: \(error.localizedDescription)"
            )
        }
    }

    // MARK: - Request building

    /// Builds `<apiBaseURL>/<path>`.
    private func makeURL(path: String) throws -> URL {
        guard let base = baseURL else {
            throw ControlPlaneError.invalidConfiguration(
                message: "Could not build the control-plane URL from apiBaseURL: \(config.apiBaseURL)"
            )
        }
        return base.appending(path: path)
    }

    // MARK: - Transport

    /// The `X-API-Key` header is required by the control plane (sent by
    /// ``GatewayHTTP``). A JSON body is sent with `Content-Type:
    /// application/json`.
    private func send(method: String, url: URL, body: Data?) async throws -> Data {
        let data: Data
        let httpResponse: HTTPURLResponse
        do {
            (data, httpResponse) = try await GatewayHTTP.send(
                method: method,
                url: url,
                apiKey: config.apiKey,
                body: body,
                contentType: body == nil ? nil : "application/json"
            )
        } catch let transport as GatewayHTTP.TransportError {
            switch transport.reason {
            case let .network(underlying):
                Log.net.error("transport \(url): \(underlying)")
                throw ControlPlaneError.transport(
                    message: "Could not reach the control plane at \(config.apiBaseURL): \(underlying)"
                )
            case .noHTTPResponse:
                throw ControlPlaneError.transport(message: "Control plane returned no HTTP response.")
            }
        }

        guard (200 ..< 300).contains(httpResponse.statusCode) else {
            let serverMessage = (try? JSONDecoder().decode(ErrorResponse.self, from: data))?.message
                ?? HTTPURLResponse.localizedString(forStatusCode: httpResponse.statusCode)
            Log.net.error("HTTP \(httpResponse.statusCode) \(url.absoluteString): \(serverMessage)")
            throw ControlPlaneError.httpStatus(code: httpResponse.statusCode, message: serverMessage)
        }
        return data
    }

    // MARK: - Request/response models

    /// Error body returned by the control plane on failure. The gateway
    /// answers `{"error": "..."}`; `detail` is kept for legacy FastAPI
    /// responses.
    private struct ErrorResponse: Decodable {
        let error: String?
        let detail: String?

        var message: String? { error ?? detail }
    }
}
