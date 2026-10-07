import Foundation

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
///
/// Fetching seam for the agent picker, so ``AgentStore`` is testable without a
/// network. ``ControlPlaneClient`` is the live implementation.
protocol AgentFetching: Sendable {
    func listAgents() async throws -> [Agent]
}

struct ControlPlaneClient: AgentFetching {
    let config: MemoryConfig

    /// The control-plane base URL from `config.apiBaseURL`.
    var baseURL: URL? {
        URL(string: config.apiBaseURL)
    }

    // MARK: - Agents

    /// Fetches all configured agents (summary shape).
    func listAgents() async throws -> [Agent] {
        let url = try makeURL(path: "api/agents")
        let data = try await send(url: url)
        do {
            return try JSONDecoder().decode([Agent].self, from: data)
        } catch {
            throw GatewayError.decode(
                "Control plane returned an unexpected agent list: \(error.localizedDescription)"
            )
        }
    }

    // MARK: - Request building

    /// Builds `<apiBaseURL>/<path>`.
    private func makeURL(path: String) throws -> URL {
        guard let base = baseURL else {
            throw GatewayError.invalidConfiguration(
                "Could not build the control-plane URL from apiBaseURL: \(config.apiBaseURL)"
            )
        }
        return base.appending(path: path)
    }

    // MARK: - Transport

    /// The `X-API-Key` header is required by the control plane (sent by
    /// ``GatewayHTTP``).
    private var service: GatewayServiceMessages {
        GatewayServiceMessages(
            httpPrefix: "Control plane",
            noResponsePrefix: "Control plane",
            reachTarget: "the control plane at \(config.apiBaseURL)"
        )
    }

    private func send(url: URL) async throws -> Data {
        let request = GatewayHTTP.makeRequest(method: "GET", url: url, apiKey: config.apiKey)
        return try await GatewayHTTP.sendChecked(request, service: service)
    }
}
