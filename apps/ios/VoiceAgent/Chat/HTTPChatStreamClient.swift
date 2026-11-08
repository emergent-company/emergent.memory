import Foundation

// MARK: - Streaming seam

/// Opens the gateway's chat stream and yields its raw SSE `data:` payloads.
///
/// A seam rather than a concrete client so the transport (and its frame
/// mapping) is testable without a network: tests feed recorded frames.
protocol ChatStreaming: Sendable {
    func frames(_ turn: ChatTurnRequest) -> AsyncThrowingStream<String, Error>
}

/// The live implementation: `POST {gateway}/api/chat` with the device
/// credential, `Accept: text/event-stream`.
///
/// This is the same endpoint the web chat uses. The app used to reach text only
/// through a LiveKit room, which needed a token endpoint, a LiveKit server and a
/// bridge worker — none of which exist on a deployment without a voice stack.
struct HTTPChatStreamClient: ChatStreaming {
    let config: MemoryConfig
    var session: URLSession = .shared

    func frames(_ turn: ChatTurnRequest) -> AsyncThrowingStream<String, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    let request = try makeRequest(turn)
                    let (bytes, response) = try await session.bytes(for: request)
                    guard let http = response as? HTTPURLResponse else {
                        throw GatewayError.transport("The chat stream returned no HTTP response.")
                    }
                    guard (200 ..< 300).contains(http.statusCode) else {
                        var body = Data()
                        for try await line in bytes.lines {
                            body.append(Data(line.utf8))
                        }
                        throw GatewayError.httpStatus(http.statusCode, Self.errorMessage(body, status: http.statusCode))
                    }

                    var parser = SSEFrameParser()
                    for try await line in bytes.lines {
                        if Task.isCancelled {
                            break
                        }
                        if let payload = parser.consume(line) {
                            continuation.yield(payload)
                        }
                    }
                    if !Task.isCancelled, let payload = parser.finish() {
                        continuation.yield(payload)
                    }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    private func makeRequest(_ turn: ChatTurnRequest) throws -> URLRequest {
        guard let base = config.gatewayBaseURL else {
            throw GatewayError.invalidConfiguration(
                "Could not derive the chat URL from the token endpoint: \(config.tokenEndpoint)"
            )
        }
        let url = base.appending(path: "api").appending(path: "chat")
        let body = try JSONEncoder().encode(ChatTurnBody(turn))
        return GatewayHTTP.makeRequest(
            method: "POST",
            url: url,
            apiKey: config.apiKey,
            body: body,
            contentType: "application/json",
            extraHeaders: [
                "Accept": "text/event-stream",
                "Cache-Control": "no-cache",
            ],
            timeout: 600
        )
    }

    /// Mirrors the gateway's `{"error": "..."}` body when present.
    private static func errorMessage(_ body: Data, status: Int) -> String {
        if let object = try? JSONSerialization.jsonObject(with: body) as? [String: Any],
           let message = object["error"] as? String ?? object["detail"] as? String
        {
            return "Chat returned HTTP \(status): \(message)"
        }
        return "Chat returned HTTP \(status): \(HTTPURLResponse.localizedString(forStatusCode: status))"
    }
}
