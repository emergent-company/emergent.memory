import Foundation

/// Fetching seam for the session list/detail stores, so ``SessionsStore`` and
/// ``SessionDetailStore`` are testable without a network. ``SessionLogClient``
/// is the live implementation.
protocol SessionLogFetching: Sendable {
    /// Lists recorded sessions, optionally filtered to one agent definition and
    /// optionally including archived conversations.
    func listSessions(agentID: String?, includeArchived: Bool) async throws -> [SessionSummary]
    /// Full session detail (metadata, pending approvals, records).
    func detail(room: String) async throws -> SessionDetail
    /// The chronological record timeline for `room`.
    func timeline(room: String) async throws -> [SessionRecord]
    /// Archives a conversation (hidden from the default list).
    func archiveConversation(id: String) async throws
    /// Restores an archived conversation.
    func unarchiveConversation(id: String) async throws
    /// Permanently deletes a conversation.
    func deleteConversation(id: String) async throws
    /// Answers a pending tool approval from a resumed session.
    func respondQuestion(questionID: String, response: String, message: String) async throws
}

extension SessionLogFetching {
    /// Convenience for callers that want the unfiltered, unarchived list.
    func listSessions() async throws -> [SessionSummary] {
        try await listSessions(agentID: nil, includeArchived: false)
    }

    /// Convenience for callers that only filter by agent (archived excluded).
    func listSessions(agentID: String?) async throws -> [SessionSummary] {
        try await listSessions(agentID: agentID, includeArchived: false)
    }

    /// The timeline is the detail's record list; kept for existing callers.
    func timeline(room: String) async throws -> [SessionRecord] {
        try await detail(room: room).records
    }
}

struct SessionLogClient: SessionLogFetching {
    let config: MemoryConfig
    /// Injectable transport for unit tests; production uses the shared session.
    var session: URLSession = .shared

    /// The session-log base URL: the gateway host root derived from the token
    /// endpoint (see ``MemoryConfig/gatewayBaseURL``).
    var sessionsBaseURL: URL? {
        config.gatewayBaseURL
    }

    /// Lists recorded sessions, most recently active first.
    func listSessions(agentID: String?, includeArchived: Bool) async throws -> [SessionSummary] {
        let url = try makeURL(
            path: "api/sessions",
            room: nil,
            agentID: agentID,
            includeArchived: includeArchived
        )
        let data = try await send(url: url)
        let response: SessionsResponse
        do {
            response = try JSONDecoder().decode(SessionsResponse.self, from: data)
        } catch {
            throw GatewayError.decode(
                "Session log returned an unexpected session list: \(error.localizedDescription)"
            )
        }
        return response.sessions
    }

    /// Returns the full detail for `room` (metadata, approvals, records).
    func detail(room: String) async throws -> SessionDetail {
        let url = try makeURL(path: "api/session", room: room, agentID: nil)
        let data = try await send(url: url)
        do {
            return try JSONDecoder().decode(SessionDetail.self, from: data)
        } catch {
            throw GatewayError.decode(
                "Session log returned an unexpected session timeline: \(error.localizedDescription)"
            )
        }
    }

    /// Returns the chronological record timeline for `room`.
    func timeline(room: String) async throws -> [SessionRecord] {
        try await detail(room: room).records
    }

    // MARK: - Conversation lifecycle

    func archiveConversation(id: String) async throws {
        try await postConversationAction(id: id, suffix: "archive")
    }

    func unarchiveConversation(id: String) async throws {
        try await postConversationAction(id: id, suffix: "unarchive")
    }

    func deleteConversation(id: String) async throws {
        let url = try conversationURL(id: id, suffix: nil)
        _ = try await send(url: url, method: "DELETE")
    }

    /// Answers a pending tool approval that predates the live room. Memory
    /// resumes the paused run in the background; the caller reloads the
    /// transcript to see the continuation.
    func respondQuestion(questionID: String, response: String, message: String) async throws {
        let url = try questionActionURL(questionID: questionID, action: "respond")
        let body = try JSONEncoder().encode(RespondQuestionBody(response: response, message: message))
        let request = GatewayHTTP.makeRequest(
            method: "POST",
            url: url,
            apiKey: config.apiKey,
            body: body,
            contentType: "application/json"
        )
        _ = try await GatewayHTTP.sendChecked(request, service: service, session: session)
    }

    private func postConversationAction(id: String, suffix: String) async throws {
        let url = try conversationURL(id: id, suffix: suffix)
        _ = try await send(url: url, method: "POST")
    }

    // MARK: - Request building

    /// Builds `<sessionsBaseURL>/<path>?room=<room>&agentId=<agent>&includeArchived=true`
    /// (all query values optional).
    private func makeURL(
        path: String,
        room: String?,
        agentID: String?,
        includeArchived: Bool = false
    ) throws -> URL {
        guard let base = sessionsBaseURL else {
            throw GatewayError.invalidConfiguration(
                "Could not derive the session-log URL from the token endpoint: \(config.tokenEndpoint)"
            )
        }
        let url = base.appending(path: path)
        var queryItems: [URLQueryItem] = []
        if let room {
            queryItems.append(URLQueryItem(name: "room", value: room))
        }
        if let agentID, !agentID.isEmpty {
            queryItems.append(URLQueryItem(name: "agentId", value: agentID))
        }
        if includeArchived {
            queryItems.append(URLQueryItem(name: "includeArchived", value: "true"))
        }
        guard !queryItems.isEmpty else { return url }
        guard var components = URLComponents(url: url, resolvingAgainstBaseURL: false) else {
            throw GatewayError.invalidConfiguration("Could not build the session-log URL.")
        }
        components.queryItems = queryItems
        guard let finalURL = components.url else {
            throw GatewayError.invalidConfiguration("Could not build the session-log URL.")
        }
        return finalURL
    }

    /// Builds `<sessionsBaseURL>/api/chat/questions/<id>/<action>`.
    private func questionActionURL(questionID: String, action: String) throws -> URL {
        guard let base = sessionsBaseURL else {
            throw GatewayError.invalidConfiguration(
                "Could not derive the session-log URL from the token endpoint: \(config.tokenEndpoint)"
            )
        }
        return base
            .appending(path: "api")
            .appending(path: "chat")
            .appending(path: "questions")
            .appending(path: questionID)
            .appending(path: action)
    }

    /// Builds `<sessionsBaseURL>/api/conversations/<id>[/<suffix>]`.
    private func conversationURL(id: String, suffix: String?) throws -> URL {
        guard let base = sessionsBaseURL else {
            throw GatewayError.invalidConfiguration(
                "Could not derive the session-log URL from the token endpoint: \(config.tokenEndpoint)"
            )
        }
        var url = base
            .appending(path: "api")
            .appending(path: "conversations")
            .appending(path: id)
        if let suffix {
            url = url.appending(path: suffix)
        }
        return url
    }

    // MARK: - Transport

    /// The `X-API-Key` header is required by the session log, same as the
    /// token endpoint (sent by ``GatewayHTTP``).
    private var service: GatewayServiceMessages {
        GatewayServiceMessages(
            httpPrefix: "Session log",
            noResponsePrefix: "Session log",
            reachTarget: "the session log at \(config.tokenEndpoint)"
        )
    }

    private func send(url: URL, method: String = "GET") async throws -> Data {
        let request = GatewayHTTP.makeRequest(method: method, url: url, apiKey: config.apiKey)
        return try await GatewayHTTP.sendChecked(request, service: service, session: session)
    }

    // MARK: - Response models

    private struct SessionsResponse: Decodable {
        let sessions: [SessionSummary]
    }

    private struct RespondQuestionBody: Encodable {
        let response: String
        let message: String
    }
}
