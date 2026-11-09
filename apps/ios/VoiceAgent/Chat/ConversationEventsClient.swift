import Foundation

/// Opens the gateway's conversation events stream and yields decoded frames.
///
/// A seam rather than a concrete client so the reconnect loop and frame mapping
/// are testable without a network.
protocol ConversationEventStreaming: Sendable {
    func events(conversationID: String) -> AsyncThrowingStream<ChatStreamEvent, Error>
}

/// `GET {gateway}/api/conversations/{id}/events` with the device credential,
/// `Accept: text/event-stream`.
///
/// This is the app's resync channel: the server emits a populated `refresh`
/// frame on every connect, so a dropped stream needs no separate recovery path.
/// The heartbeat is every 25s and the request idles out at 60s, so silence for a
/// minute means the connection is dead and the loop reconnects. Connection
/// failures are swallowed and retried with exponential backoff (1, 2, 4, 8, 15,
/// 15… seconds); only a configuration that can never work ends the stream.
struct ConversationEventsClient: ConversationEventStreaming {
    let config: MemoryConfig
    var session: URLSession = .shared

    func events(conversationID: String) -> AsyncThrowingStream<ChatStreamEvent, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    // Building the request only fails on a configuration the
                    // reconnect loop could never fix, so it stays outside it.
                    let request = try makeRequest(conversationID: conversationID)
                    await pump(request: request, continuation: continuation)
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    private func makeRequest(conversationID: String) throws -> URLRequest {
        guard let base = config.gatewayBaseURL else {
            throw GatewayError.invalidConfiguration(
                "Could not derive the conversation events URL from the token endpoint: \(config.tokenEndpoint)"
            )
        }
        let url = base
            .appending(path: "api")
            .appending(path: "conversations")
            .appending(path: conversationID)
            .appending(path: "events")
        return GatewayHTTP.makeRequest(
            method: "GET",
            url: url,
            apiKey: config.apiKey,
            extraHeaders: [
                "Accept": "text/event-stream",
                "Cache-Control": "no-cache",
            ],
            timeout: 60
        )
    }

    /// Reconnects until the surrounding task is cancelled. A dropped socket or
    /// idle timeout is not an error the UI should show: the next connection's
    /// `refresh` frame resyncs the run facts, so failures are swallowed here
    /// rather than surfaced as stream errors.
    private func pump(
        request: URLRequest,
        continuation: AsyncThrowingStream<ChatStreamEvent, Error>.Continuation
    ) async {
        var backoff: TimeInterval = 1
        while !Task.isCancelled {
            do {
                try await connect(request: request, continuation: continuation)
            } catch is CancellationError {
                return
            } catch {
                // Transient connection failure: retry after the backoff.
                if Task.isCancelled {
                    return
                }
            }
            do {
                try await Task.sleep(for: .seconds(backoff))
            } catch {
                return
            }
            backoff = min(backoff * 2, 15)
        }
    }

    /// One connection attempt: yields frames until the server closes the stream
    /// or the task is cancelled, then returns so the caller reconnects.
    private func connect(
        request: URLRequest,
        continuation: AsyncThrowingStream<ChatStreamEvent, Error>.Continuation
    ) async throws {
        let (bytes, response) = try await session.bytes(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw GatewayError.transport("The conversation events stream returned no HTTP response.")
        }
        guard (200 ..< 300).contains(http.statusCode) else {
            var body = Data()
            for try await line in bytes.lines {
                body.append(Data(line.utf8))
            }
            throw GatewayError.httpStatus(
                http.statusCode,
                Self.errorMessage(body, status: http.statusCode)
            )
        }

        var parser = SSEFrameParser()
        // Only used for `mcp_tool` frames that lack an id in a `live_replay`;
        // the counter is per connection so such pairs stay correlated.
        var toolSequence = 0
        for try await line in bytes.lines {
            if Task.isCancelled {
                break
            }
            guard let payload = parser.consume(line) else { continue }
            guard let event = ChatStreamFrameDecoder.decode(
                payload,
                fallbackToolID: {
                    toolSequence += 1
                    return "events-tool-\(toolSequence)"
                }
            ) else { continue }
            continuation.yield(event)
        }
        if !Task.isCancelled, let payload = parser.finish() {
            if let event = ChatStreamFrameDecoder.decode(
                payload,
                fallbackToolID: {
                    toolSequence += 1
                    return "events-tool-\(toolSequence)"
                }
            ) {
                continuation.yield(event)
            }
        }
    }

    /// Mirrors the gateway's `{"error": "..."}` body when present.
    private static func errorMessage(_ body: Data, status: Int) -> String {
        if let object = try? JSONSerialization.jsonObject(with: body) as? [String: Any],
           let message = object["error"] as? String ?? object["detail"] as? String
        {
            return "Conversation events returned HTTP \(status): \(message)"
        }
        return "Conversation events returned HTTP \(status): \(HTTPURLResponse.localizedString(forStatusCode: status))"
    }
}
