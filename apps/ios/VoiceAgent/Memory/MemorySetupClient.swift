import Foundation

/// One-time QR onboarding exchange: POSTs the setup token to the setup
/// endpoint and gets back the per-device server URL, token endpoint, API base
/// URL, and API key.
///
/// Unlike the control-plane client, no `X-API-Key` header is sent — the
/// device does not have a key yet; the token itself authorizes the exchange.
struct MemorySetupClient: Sendable {
    /// Request body: `{"token": "<32-hex>", "device": {...}}`. The `device`
    /// self-introduction manifest is optional; a nil manifest omits the key so
    /// the body matches the pre-manifest exchange exactly.
    struct Payload: Codable {
        let token: String
        let device: DeviceManifest?

        init(token: String, device: DeviceManifest? = nil) {
            self.token = token
            self.device = device
        }
    }

    /// Response body (200): per-device configuration.
    struct Response: Decodable {
        let serverURL: String
        let tokenEndpoint: String
        let apiBaseURL: String
        let apiKey: String
        /// `"client"` = worker is text-only (app speaks with AVSpeechSynthesizer),
        /// `"server"` = worker streams its own TTS audio. Optional for compat
        /// with gateways that predate the field.
        let ttsStrategy: String?
        /// Project identity the device credential was minted under. Optional
        /// for compat with gateways that predate multi-project support.
        let orgID: String?
        let projectID: String?
        let projectName: String?
    }

    /// POSTs `token` (plus the optional device self-introduction manifest) to
    /// `url` and returns the per-device configuration.
    func setup(url: URL, token: String, device: DeviceManifest? = nil) async throws -> Response {
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        do {
            request.httpBody = try JSONEncoder().encode(Payload(token: token, device: device))
        } catch {
            throw GatewayError.transport(
                "Could not encode the setup request: \(error.localizedDescription)"
            )
        }

        let data = try await GatewayHTTP.sendChecked(
            request,
            service: GatewayServiceMessages(
                httpPrefix: "Setup",
                noResponsePrefix: "The setup endpoint",
                reachTarget: "the setup endpoint at \(url.absoluteString)",
                logsErrors: false,
                parseErrorBody: false
            )
        )

        do {
            return try JSONDecoder().decode(Response.self, from: data)
        } catch {
            throw GatewayError.decode(
                "The setup endpoint returned an unexpected response: \(error.localizedDescription)"
            )
        }
    }
}
