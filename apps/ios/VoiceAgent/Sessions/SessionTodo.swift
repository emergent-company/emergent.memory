import Foundation

/// One item of the agent's plan for a conversation's ACP session, as returned by
/// `GET /api/conversations/:id/todos` (`emergent.memory#440`).
///
/// The status stays the server's raw string rather than an enum: the web card
/// renders whatever it is given, and a status this build does not know
/// (`blocked`, a future state) must still show its text instead of vanishing or
/// failing to decode the whole plan. ``knownStatus`` names the three the chat UI
/// draws a glyph for.
struct SessionTodo: Codable, Identifiable, Equatable, Sendable {
    let id: String
    let content: String
    let status: String
    /// The server's position for the item; the plan renders in this order.
    let order: Int

    /// The status when it is one of the three the iOS card draws a glyph for,
    /// nil for anything else (which renders as plain text).
    var knownStatus: SessionTodoStatus? {
        SessionTodoStatus(rawValue: status)
    }
}

/// The plan states the chat card knows how to draw. The raw values are the
/// gateway's/ACP's wire strings.
enum SessionTodoStatus: String, Sendable {
    case pending
    case inProgress = "in_progress"
    case completed
}

/// The plan route's body: `{"todos":[…]}`. `todos` is always present and always
/// an array on the gateway (a lookup failure is an empty list, never an error),
/// so a missing key is the only thing that fails to decode.
struct SessionTodosResponse: Decodable, Equatable, Sendable {
    let todos: [SessionTodo]
}

/// Fetching seam for the plan card, so ``ChatPlanStore`` is testable without a
/// network. ``SessionLogClient`` is the live implementation.
protocol SessionTodoFetching: Sendable {
    /// The conversation's session plan, ordered as the server returns it.
    func todos(conversationID: String) async throws -> [SessionTodo]
}

extension SessionLogClient: SessionTodoFetching {
    /// `GET /api/conversations/:id/todos` — the JSON twin of the web chat's
    /// `/partial/chat-todos` card, so the phone can show the same plan.
    ///
    /// The route answers 200 with an empty list when the conversation has no
    /// session, no plan, or is not the caller's to see, so a 2xx here always
    /// decodes. A transport or non-2xx failure throws and the store keeps the
    /// last plan it showed.
    func todos(conversationID: String) async throws -> [SessionTodo] {
        guard let base = sessionsBaseURL else {
            throw GatewayError.invalidConfiguration(
                "Could not derive the plan URL from the token endpoint: \(config.tokenEndpoint)"
            )
        }
        let url = base
            .appending(path: "api")
            .appending(path: "conversations")
            .appending(path: conversationID)
            .appending(path: "todos")
        let request = GatewayHTTP.makeRequest(method: "GET", url: url, apiKey: config.apiKey)
        let data = try await GatewayHTTP.sendChecked(request, service: todoService, session: session)
        do {
            return try JSONDecoder().decode(SessionTodosResponse.self, from: data).todos
        } catch {
            throw GatewayError.decode(
                "The plan route returned an unexpected body: \(error.localizedDescription)"
            )
        }
    }

    /// Names the session log (not the token route the URL derives from) in a
    /// failure, the same as the client's other reads.
    private var todoService: GatewayServiceMessages {
        GatewayServiceMessages(
            httpPrefix: "Plan",
            noResponsePrefix: "Plan",
            reachTarget: "the session log at \(sessionsBaseURL?.absoluteString ?? config.tokenEndpoint)"
        )
    }
}
