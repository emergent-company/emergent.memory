import Foundation

/// The device-facing slice of project configuration (`GET /api/project/config`).
///
/// Deliberately tiny: voice enablement only. The gateway endpoint never returns
/// secrets, endpoints or provider settings.
struct ProjectConfig: Codable, Equatable, Sendable {
    /// Whether the project has voice (the call/audio surface) turned on.
    let voiceEnabled: Bool
}

/// The fetch seam, so the store is testable without a gateway.
protocol ProjectConfigFetching: Sendable {
    func fetch() async throws -> ProjectConfig
}

/// Live client for `GET /api/project/config` on the device surface.
///
/// Uses the same gateway root and `X-API-Key` device credential as the session
/// log (``MemoryConfig/gatewayBaseURL``).
struct ProjectConfigClient: ProjectConfigFetching {
    let config: MemoryConfig

    func fetch() async throws -> ProjectConfig {
        guard let base = config.gatewayBaseURL else {
            throw GatewayError.invalidConfiguration(
                "Could not derive the gateway URL from the token endpoint: \(config.tokenEndpoint)"
            )
        }
        let url = base.appending(path: "api/project/config")
        let request = GatewayHTTP.makeRequest(method: "GET", url: url, apiKey: config.apiKey)
        let data = try await GatewayHTTP.sendChecked(
            request,
            service: GatewayServiceMessages(
                httpPrefix: "Project config",
                noResponsePrefix: "Project config",
                reachTarget: "the gateway at \(config.tokenEndpoint)"
            )
        )
        do {
            return try JSONDecoder().decode(ProjectConfig.self, from: data)
        } catch {
            throw GatewayError.decode(
                "Project config returned an unexpected payload: \(error.localizedDescription)"
            )
        }
    }
}
