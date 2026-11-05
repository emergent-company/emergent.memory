import Foundation
@testable import Memory
import Testing

/// A `URLProtocol` stub local to the session-log tests (a separate handler from
/// `GatewayHTTPStubProtocol`, so the suites cannot stomp on each other).
final nonisolated class SessionLogStubProtocol: URLProtocol {
    nonisolated(unsafe) static var handler: (@Sendable (URLRequest) throws -> (URLResponse, Data))?

    override static func canInit(with _: URLRequest) -> Bool {
        true
    }

    override static func canonicalRequest(for request: URLRequest) -> URLRequest {
        request
    }

    override func startLoading() {
        guard let handler = Self.handler else {
            client?.urlProtocol(self, didFailWithError: URLError(.unsupportedURL))
            return
        }
        do {
            let (response, data) = try handler(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}
}

struct SessionLogClientTests {
    @Test func sessionsBaseURLStripsTokenPath() {
        var config = MemoryConfig()
        config.tokenEndpoint = "http://host:8080/api/token"
        let client = SessionLogClient(config: config)
        #expect(client.sessionsBaseURL == URL(string: "http://host:8080"))
    }

    /// The shared derivation strips a `/api/token` suffix, but a deployment may
    /// configure any token URL. The fallback (drop the last two path components)
    /// is what keeps the session log and the project config pointing at the
    /// same root in that case, so pin it.
    @Test func gatewayBaseURLFallsBackForANonTokenEndpoint() {
        var config = MemoryConfig()
        config.tokenEndpoint = "http://host:8080/v1/auth/token"
        // The fallback drops the token endpoint's last two path components;
        // compare host + path so the assertion does not hinge on a trailing slash.
        #expect(config.gatewayBaseURL?.host == "host")
        #expect(config.gatewayBaseURL?.path == "/v1")
    }

    @Test func sessionsBaseURLUsesTheSameFallback() {
        var config = MemoryConfig()
        config.tokenEndpoint = "http://host:8080/v1/auth/token"
        let client = SessionLogClient(config: config)
        #expect(client.sessionsBaseURL?.path == "/v1")
    }

    @Test func decodesTurnRecord() throws {
        let json = #"{"ts":1,"clock":"10:00:00.000","room":"memory-persistent","variant":"google-rt","kind":"turn","role":"user","text":"hello"}"#
        let record = try JSONDecoder().decode(SessionRecord.self, from: Data(json.utf8))
        #expect(record.kind == "turn")
        #expect(record.role == "user")
        #expect(record.text == "hello")
    }

    @Test func decodesToolCallWithFlexibleArgs() throws {
        let json = #"{"name":"turn_on","args":{"entity_id":"light.lamp"}}"#
        let call = try JSONDecoder().decode(ToolCall.self, from: Data(json.utf8))
        #expect(call.name == "turn_on")
        #expect(call.arguments?.contains("light.lamp") == true)
    }

    @Test func decodesToolsExecutedFlatteningOutputs() throws {
        let json = #"{"kind":"tools_executed","calls":[{"name":"turn_on","args":{"entity_id":"light.lamp"}}],"outputs":[{"output":"done","is_error":false}]}"#
        let record = try JSONDecoder().decode(SessionRecord.self, from: Data(json.utf8))
        #expect(record.kind == "tools_executed")
        #expect(record.calls?.first?.name == "turn_on")
        #expect(record.outputs == ["done"])
        #expect(record.isError == nil)
    }

    @Test func decodesSummaryOriginAgentAndTitle() throws {
        let json = #"{"room":"conv-1","origin":"manual","started_at":"2026-09-01T10:00:00Z","ended_at":null,"turns":2,"tool_calls":1,"preview":"hi","agent_definition_id":"agent-a","title":"Greeting"}"#
        let summary = try JSONDecoder().decode(SessionSummary.self, from: Data(json.utf8))
        #expect(summary.room == "conv-1")
        #expect(summary.origin == "manual")
        #expect(summary.agentDefinitionID == "agent-a")
        #expect(summary.title == "Greeting")
    }

    @Test func decodesSummaryToleratingAbsentFields() throws {
        let json = #"{"room":"conv-1","turns":0,"tool_calls":0}"#
        let summary = try JSONDecoder().decode(SessionSummary.self, from: Data(json.utf8))
        #expect(summary.origin == nil)
        #expect(summary.agentDefinitionID == nil)
        #expect(summary.title == nil)
        #expect(summary.startedAt == nil)
        #expect(summary.isArchived == false)
    }

    @Test func decodesSummaryArchivedFlag() throws {
        let archived = try JSONDecoder().decode(
            SessionSummary.self,
            from: Data(#"{"room":"conv-1","turns":0,"tool_calls":0,"is_archived":true}"#.utf8)
        )
        #expect(archived.isArchived == true)
    }

    @Test func decodesDetailPendingApprovalsAndRecords() throws {
        let json = #"""
        {"room":"conv-1","agent_definition_id":"agent-a","title":"Greeting",
         "pending_approvals":[{"questionId":"q1","tool":"search","input":{"query":"x","limit":3}}],
         "records":[{"kind":"turn","role":"user","text":"hello"},
                    {"kind":"tools_executed","calls":[{"name":"search","args":{"query":"x"}}],"outputs":["ok"]}]}
        """#
        let detail = try JSONDecoder().decode(SessionDetail.self, from: Data(json.utf8))
        #expect(detail.room == "conv-1")
        #expect(detail.agentDefinitionID == "agent-a")
        #expect(detail.title == "Greeting")
        #expect(detail.pendingApprovals.count == 1)
        #expect(detail.pendingApprovals[0].questionID == "q1")
        #expect(detail.pendingApprovals[0].tool == "search")
        #expect(detail.pendingApprovals[0].arguments.contains("\"query\":\"x\""))
        #expect(detail.records.count == 2)
        #expect(detail.records[0].kind == "turn")
        #expect(detail.records[1].kind == "tools_executed")
    }

    @Test func decodesDetailToleratingAbsentPendingApprovals() throws {
        let json = #"{"room":"conv-1","records":[]}"#
        let detail = try JSONDecoder().decode(SessionDetail.self, from: Data(json.utf8))
        #expect(detail.pendingApprovals.isEmpty)
        #expect(detail.records.isEmpty)
        #expect(detail.agentDefinitionID == nil)
    }
}

/// URLProtocol exposes a request body as a stream, not as `httpBody`; read it
/// so request-shape tests can assert the JSON payload.
private nonisolated func requestBodyData(_ request: URLRequest) -> Data? {
    if let body = request.httpBody {
        return body
    }
    guard let stream = request.httpBodyStream else { return nil }
    stream.open()
    defer { stream.close() }
    var data = Data()
    let size = 4096
    let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: size)
    defer { buffer.deallocate() }
    while stream.hasBytesAvailable {
        let read = stream.read(buffer, maxLength: size)
        if read <= 0 {
            break
        }
        data.append(buffer, count: read)
    }
    return data
}

/// URL-level tests that pin request shape (query params and lifecycle paths).
@Suite(.serialized) struct SessionLogClientRequestTests {
    /// Mutable request capture shared with the nonisolated URLProtocol handler.
    private final class RequestRecorder: @unchecked Sendable {
        nonisolated(unsafe) var urls: [URL] = []
        nonisolated(unsafe) var requests: [(method: String, path: String)] = []
        nonisolated(unsafe) var bodies: [Data?] = []
    }

    private func makeClient(
        recorder: RequestRecorder
    ) -> SessionLogClient {
        var config = MemoryConfig()
        config.tokenEndpoint = "http://host:8080/api/token"
        config.apiKey = "secret"
        SessionLogStubProtocol.handler = { request in
            recorder.urls.append(request.url!)
            recorder.requests.append((request.httpMethod ?? "", request.url?.path ?? ""))
            recorder.bodies.append(requestBodyData(request))
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: nil,
                headerFields: nil
            )!
            if request.url?.path == "/api/sessions" {
                return (response, Data(#"{"sessions":[]}"#.utf8))
            }
            return (response, Data(#"{"room":"conv-1","records":[]}"#.utf8))
        }
        let session = URLSessionConfiguration.ephemeral
        session.protocolClasses = [SessionLogStubProtocol.self]
        return SessionLogClient(
            config: config,
            session: URLSession(configuration: session)
        )
    }

    @Test func listSessionsAppendsAgentQueryWhenSet() async throws {
        let recorder = RequestRecorder()
        let client = makeClient(recorder: recorder)
        defer { SessionLogStubProtocol.handler = nil }

        _ = try await client.listSessions(agentID: "agent-a")

        let url = try #require(recorder.urls.first)
        let components = try #require(URLComponents(url: url, resolvingAgainstBaseURL: false))
        #expect(components.path == "/api/sessions")
        #expect(components.queryItems?.contains(URLQueryItem(name: "agentId", value: "agent-a")) == true)
    }

    @Test func listSessionsAppendsAgentAndArchivedQueries() async throws {
        let recorder = RequestRecorder()
        let client = makeClient(recorder: recorder)
        defer { SessionLogStubProtocol.handler = nil }

        _ = try await client.listSessions(agentID: "agent-a", includeArchived: true)

        let url = try #require(recorder.urls.first)
        let components = try #require(URLComponents(url: url, resolvingAgainstBaseURL: false))
        let items = components.queryItems ?? []
        #expect(items.contains(URLQueryItem(name: "agentId", value: "agent-a")))
        #expect(items.contains(URLQueryItem(name: "includeArchived", value: "true")))
    }

    @Test func listSessionsOmitsAgentQueryWhenNilOrEmpty() async throws {
        let recorder = RequestRecorder()
        let client = makeClient(recorder: recorder)
        defer { SessionLogStubProtocol.handler = nil }

        _ = try await client.listSessions(agentID: nil)
        _ = try await client.listSessions(agentID: "")

        #expect(recorder.urls.count == 2)
        for url in recorder.urls {
            #expect(URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems == nil)
        }
    }

    @Test func respondQuestionPostsGatewayPayload() async throws {
        let recorder = RequestRecorder()
        let client = makeClient(recorder: recorder)
        defer { SessionLogStubProtocol.handler = nil }

        try await client.respondQuestion(
            questionID: "q-1",
            response: "reject",
            message: "not now"
        )

        let seen = try #require(recorder.requests.first)
        #expect(seen.method == "POST")
        #expect(seen.path == "/api/chat/questions/q-1/respond")
        let body = try #require(recorder.bodies.first ?? nil)
        let json = try #require(
            try JSONSerialization.jsonObject(with: body) as? [String: String]
        )
        #expect(json["response"] == "reject")
        #expect(json["message"] == "not now")
    }

    @Test func archiveAndDeleteUseConversationPaths() async throws {
        let recorder = RequestRecorder()
        let client = makeClient(recorder: recorder)
        defer { SessionLogStubProtocol.handler = nil }

        try await client.archiveConversation(id: "conv-1")
        try await client.unarchiveConversation(id: "conv-1")
        try await client.deleteConversation(id: "conv-1")

        let seen = recorder.requests
        #expect(seen.count == 3)
        #expect(seen[0].method == "POST")
        #expect(seen[0].path == "/api/conversations/conv-1/archive")
        #expect(seen[1].method == "POST")
        #expect(seen[1].path == "/api/conversations/conv-1/unarchive")
        #expect(seen[2].method == "DELETE")
        #expect(seen[2].path == "/api/conversations/conv-1")
    }

    /// The URL is derived from the token endpoint, but a session-log failure
    /// must name the session log's own host — naming `/api/token` made it read
    /// as a token failure.
    @Test func transportFailureNamesTheSessionLogHostNotTheTokenEndpoint() async throws {
        var config = MemoryConfig()
        config.tokenEndpoint = "http://host:8080/api/token"
        config.apiKey = "secret"
        SessionLogStubProtocol.handler = { _ in throw URLError(.notConnectedToInternet) }
        defer { SessionLogStubProtocol.handler = nil }

        let sessionConfig = URLSessionConfiguration.ephemeral
        sessionConfig.protocolClasses = [SessionLogStubProtocol.self]
        let client = SessionLogClient(
            config: config,
            session: URLSession(configuration: sessionConfig)
        )

        do {
            _ = try await client.detail(room: "conv-1")
            Issue.record("Expected a transport failure")
        } catch {
            let message = error.localizedDescription
            #expect(message.contains("the session log at http://host:8080"))
            #expect(!message.contains("/api/token"))
        }
    }
}
