import Foundation

/// Stopping a text turn: the local abort and the gateway's frozen cancel route.
///
/// In its own file so the transport's turn machinery stays readable; this is
/// the one place that speaks `POST /api/chat/runs/{runId}/cancel`.
extension HTTPChatTransport {
    /// Stops the turn in flight: aborts the local stream and cancels the run it
    /// belongs to on the gateway.
    ///
    /// Dropping the stream alone is not enough — the gateway runs the agent on a
    /// detached context, so the run would keep going and be recorded. With no
    /// run id (a turn that never saw `meta`: a refused request, a failed
    /// connection) nothing is sent and ``StopOutcome/noRunID`` says so instead
    /// of claiming a cancellation that did not happen.
    ///
    /// The outcome's message is raised in the composer's shared banner slot
    /// here, so the view does not keep a second copy of these strings.
    @discardableResult
    func stop() async -> StopOutcome {
        // Read the id before cancelling: the settling turn clears it.
        let runID = liveRunID
        let task = turnTask
        task?.cancel()

        // The server cancel is sent without waiting for the local settle: a
        // slow first-turn call must not delay ending the run on the server
        // (`emergent.memory#470`). The local stream still settles below.
        guard let runID else {
            await task?.value
            errorMessage = "Stopped — no active run id, so the server run could not be cancelled."
            return .noRunID
        }
        guard let base = config.gatewayBaseURL else {
            await task?.value
            let message = "Could not derive the gateway URL from the token endpoint: \(config.tokenEndpoint)"
            errorMessage = message
            return .failed(message)
        }
        let url = base
            .appending(path: "api")
            .appending(path: "chat")
            .appending(path: "runs")
            .appending(path: runID)
            .appending(path: "cancel")
        let request = GatewayHTTP.makeRequest(
            method: "POST",
            url: url,
            apiKey: config.apiKey,
            body: Data("{}".utf8),
            contentType: "application/json"
        )
        do {
            let (data, http) = try await GatewayHTTP.perform(request, session: urlSession)
            // The local stream has settled by now (or settles right after); the
            // outcome is reported only once both halves are done.
            await task?.value
            let body = try? JSONDecoder().decode(CancelRunResponse.self, from: data)
            if body?.isOK == true {
                // The turn is over, so nothing is wrong with it: clear any
                // banner left from the stream it was stopped in and hold the
                // settled "cancelling" state until the next refresh arrives.
                errorMessage = nil
                isRunning = false
                isCancelling = true
                showsCancelledNotice = true
                return .cancelled
            }
            let reason = (body?.reason ?? body?.error)?.trimmingCharacters(in: .whitespacesAndNewlines)
            let detail = reason.flatMap { $0.isEmpty ? nil : $0 } ?? "Gateway error \(http.statusCode)"
            errorMessage = "Could not cancel the server run: \(detail)"
            return .failed(detail)
        } catch is CancellationError {
            // The stop request itself was abandoned, with the view that asked
            // for it: no banner for a view that cannot show one, and never a
            // claim that the run was cancelled.
            await task?.value
            return .failed("The stop request was cancelled before the gateway answered.")
        } catch {
            await task?.value
            errorMessage = "Could not cancel the server run: \(error.localizedDescription)"
            return .failed(error.localizedDescription)
        }
    }
}

/// The frozen cancel route's answer. `"ok": true` is the only success signal; a
/// refusal carries why in `reason` (or, on a proxy error, `error`).
private struct CancelRunResponse: Decodable {
    let isOK: Bool?
    let reason: String?
    let error: String?

    private enum CodingKeys: String, CodingKey {
        case isOK = "ok"
        case reason
        case error
    }
}
