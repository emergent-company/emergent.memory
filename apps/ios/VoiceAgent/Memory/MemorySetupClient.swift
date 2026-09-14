import Foundation

/// Errors thrown by ``MemorySetupClient`` while exchanging the one-time
/// setup token for per-device configuration.
enum MemorySetupError: LocalizedError, Sendable {
    /// The endpoint answered with a non-2xx status code.
    case httpStatus(code: Int, message: String)
    /// The endpoint could not be reached at all (DNS/TCP/TLS failure).
    case transport(message: String)
    /// The endpoint answered but the body could not be decoded.
    case decode(message: String)

    var errorDescription: String? {
        switch self {
        case let .httpStatus(code, message):
            "Setup returned HTTP \(code): \(message)"
        case let .transport(message):
            message
        case let .decode(message):
            message
        }
    }
}

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
            throw MemorySetupError.transport(
                message: "Could not encode the setup request: \(error.localizedDescription)"
            )
        }

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await URLSession.shared.data(for: request)
        } catch {
            throw MemorySetupError.transport(
                message: "Could not reach the setup endpoint at \(url.absoluteString): \(error.localizedDescription)"
            )
        }

        guard let httpResponse = response as? HTTPURLResponse else {
            throw MemorySetupError.transport(message: "The setup endpoint returned no HTTP response.")
        }
        guard (200 ..< 300).contains(httpResponse.statusCode) else {
            let serverMessage = HTTPURLResponse.localizedString(forStatusCode: httpResponse.statusCode)
            throw MemorySetupError.httpStatus(code: httpResponse.statusCode, message: serverMessage)
        }

        do {
            return try JSONDecoder().decode(Response.self, from: data)
        } catch {
            throw MemorySetupError.decode(
                message: "The setup endpoint returned an unexpected response: \(error.localizedDescription)"
            )
        }
    }
}
