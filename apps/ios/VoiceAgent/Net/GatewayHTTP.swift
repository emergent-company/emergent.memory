import Foundation

/// Low-level HTTP transport shared by the gateway clients. Builds the request,
/// performs it, and returns the body plus HTTP response.
enum GatewayHTTP {
    /// Thrown when the request cannot be performed or yields no HTTP response.
    ///
    /// ``reason`` lets callers distinguish a network failure from a missing
    /// HTTP response while reusing this transport.
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
        var errorDescription: String? {
            message
        }
    }

    // MARK: - Request building

    /// Builds a `URLRequest`. When `apiKey` is nil the `X-API-Key` header is
    /// omitted (the setup exchange authorizes by token alone).
    static func makeRequest(
        method: String,
        url: URL,
        apiKey: String?,
        body: Data? = nil,
        contentType: String? = nil,
        extraHeaders: [String: String] = [:],
        timeout: TimeInterval = 30
    ) -> URLRequest {
        var request = URLRequest(url: url, timeoutInterval: timeout)
        request.httpMethod = method
        if let apiKey {
            request.setValue(apiKey, forHTTPHeaderField: "X-API-Key")
        }
        if let contentType {
            request.setValue(contentType, forHTTPHeaderField: "Content-Type")
        }
        for (key, value) in extraHeaders {
            request.setValue(value, forHTTPHeaderField: key)
        }
        request.httpBody = body
        return request
    }

    // MARK: - Transport

    /// Performs `request`, mapping `URLSession` failures to ``TransportError``.
    static func perform(
        _ request: URLRequest,
        session: URLSession
    ) async throws -> (Data, HTTPURLResponse) {
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch {
            // Cancellation is not a transport failure: the caller (or the view
            // that started the request) no longer wants the result. Rethrowing
            // the standard error keeps it recognisable downstream, where a
            // `GatewayError.transport` message would read like a network problem
            // and get shown as one.
            if Cancellation.isCancellation(error) || Cancellation.isCurrentTaskCancelled {
                throw CancellationError()
            }
            let underlying = error.localizedDescription
            throw TransportError(
                message: "Could not reach \(request.url?.absoluteString ?? "unknown"): \(underlying)",
                reason: .network(underlying: underlying)
            )
        }
        guard let http = response as? HTTPURLResponse else {
            throw TransportError(
                message: "No HTTP response from \(request.url?.absoluteString ?? "unknown").",
                reason: .noHTTPResponse
            )
        }
        return (data, http)
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
        let request = makeRequest(
            method: method,
            url: url,
            apiKey: apiKey,
            body: body,
            contentType: contentType,
            extraHeaders: extraHeaders,
            timeout: timeout
        )
        return try await perform(request, session: session)
    }

    // MARK: - Checked send

    /// Performs `request` and returns its body, mapping transport failures and
    /// non-2xx statuses to ``GatewayError`` using `service`'s phrasing. The
    /// caller owns request construction (headers included).
    static func sendChecked(
        _ request: URLRequest,
        service: GatewayServiceMessages,
        session: URLSession = .shared
    ) async throws -> Data {
        let data: Data
        let http: HTTPURLResponse
        do {
            (data, http) = try await perform(request, session: session)
        } catch let transport as TransportError {
            switch transport.reason {
            case let .network(underlying):
                if service.logsErrors {
                    Log.net.error("transport \(request.url?.absoluteString ?? "unknown"): \(underlying)")
                }
                throw GatewayError.transport("Could not reach \(service.reachTarget): \(underlying)")
            case .noHTTPResponse:
                throw GatewayError.transport("\(service.noResponsePrefix) returned no HTTP response.")
            }
        }

        guard (200 ..< 300).contains(http.statusCode) else {
            let serverMessage: String? = if service.parseErrorBody {
                (try? JSONDecoder().decode(ErrorResponse.self, from: data)).flatMap(\.message)
            } else {
                nil
            }
            let display = serverMessage ?? HTTPURLResponse.localizedString(forStatusCode: http.statusCode)
            if service.logsErrors {
                Log.net.error("HTTP \(http.statusCode) \(request.url?.absoluteString ?? "unknown"): \(display)")
            }
            throw GatewayError.httpStatus(
                http.statusCode,
                "\(service.httpPrefix) returned HTTP \(http.statusCode): \(display)"
            )
        }
        return data
    }

    /// Error body returned by a service on failure. The gateway answers
    /// `{"error": "..."}`; `detail` is kept for legacy FastAPI responses.
    private struct ErrorResponse: Decodable {
        let error: String?
        let detail: String?

        var message: String? {
            error ?? detail
        }
    }
}

/// The single error type thrown by every gateway/service client. Each case's
/// associated value is the already-composed user-facing message, so
/// `errorDescription` (and thus `localizedDescription`) matches the bespoke
/// per-service enums this replaces exactly.
enum GatewayError: Error, Equatable, Sendable {
    /// The client could not build a URL from its configuration.
    case invalidConfiguration(String)
    /// The endpoint could not be reached (or returned no HTTP response).
    case transport(String)
    /// A non-2xx status; the associated value is the fully-composed
    /// user-facing message (service prefix + status + server message).
    case httpStatus(Int, String?)
    /// The response body could not be decoded.
    case decode(String)
}

extension GatewayError: LocalizedError {
    var errorDescription: String? {
        switch self {
        case let .invalidConfiguration(message): message
        case let .transport(message): message
        case let .httpStatus(code, message): message ?? "HTTP \(code)"
        case let .decode(message): message
        }
    }
}

/// User-facing phrasing for a gateway service, used by
/// ``GatewayHTTP/sendChecked(_:service:session:)`` to reproduce each service's
/// exact error strings.
struct GatewayServiceMessages: Sendable {
    /// Prefix for "returned HTTP <code>: <message>", e.g. "Object browser".
    let httpPrefix: String
    /// Prefix for "returned no HTTP response.", e.g. "Gateway".
    let noResponsePrefix: String
    /// Target phrase for "Could not reach <target>: <underlying>", e.g.
    /// "the control plane at http://host".
    let reachTarget: String
    /// Whether transport/status failures are logged (setup does not log).
    var logsErrors: Bool = true
    /// Whether the error body is parsed for a `{"error": ...}` message.
    var parseErrorBody: Bool = true
}
