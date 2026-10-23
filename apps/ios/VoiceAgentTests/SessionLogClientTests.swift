import Foundation
@testable import Memory
import Testing

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
}
