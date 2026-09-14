import Foundation

/// Low-level HTTP transport shared by the gateway clients. Builds the request,
/// performs it, and returns the body plus HTTP response; each caller keeps its
/// own error type and status handling.
enum GatewayHTTP {
    /// Thrown when the request cannot be performed or yields no HTTP response.
    ///
    /// ``reason`` lets each caller rebuild its own error message (the
    /// service-specific "Could not reach ..." prefix / "returned no HTTP
    /// response" wording) while reusing this transport.
    struct TransportError: Error, LocalizedError {
        enum Reason: Sendable {
            /// The underlying `URLSession` call failed; carries its localized
            /// description.
            case network(underlying: String)
            /// The call succeeded but the response was not an `HTTPURLResponse`.
            case noHTTPResponse
        }

        let message: String
        let reason: Reason
        var errorDescription: String? { message }
    }

    static func send(
        method: String,
        url: URL,
        apiKey: String,
        body: Data? = nil,
        contentType: String? = nil,
        extraHeaders: [String: String] = [:],
        timeout: TimeInterval = 30,
        session: URLSession = .shared
    ) async throws -> (Data, HTTPURLResponse) {
        var request = URLRequest(url: url, timeoutInterval: timeout)
        request.httpMethod = method
        request.setValue(apiKey, forHTTPHeaderField: "X-API-Key")
        if let contentType {
            request.setValue(contentType, forHTTPHeaderField: "Content-Type")
        }
        for (key, value) in extraHeaders {
            request.setValue(value, forHTTPHeaderField: key)
        }
        request.httpBody = body

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch {
            let underlying = error.localizedDescription
            throw TransportError(
                message: "Could not reach \(url.absoluteString): \(underlying)",
                reason: .network(underlying: underlying)
            )
        }
        guard let http = response as? HTTPURLResponse else {
            throw TransportError(
                message: "No HTTP response from \(url.absoluteString).",
                reason: .noHTTPResponse
            )
        }
        return (data, http)
    }
}
