import Foundation
import Testing
@testable import Memory

@Suite struct ControlPlaneClientTests {
    @Test func baseURLComesFromApiBaseURL() {
        var config = MemoryConfig()
        config.apiBaseURL = "http://192.168.1.5:8081"
        let client = ControlPlaneClient(config: config)
        #expect(client.baseURL == URL(string: "http://192.168.1.5:8081"))
    }

    @Test func baseURLIsNilForEmptyApiBaseURL() {
        var config = MemoryConfig()
        config.apiBaseURL = ""
        let client = ControlPlaneClient(config: config)
        #expect(client.baseURL == nil)
    }

    @Test func decodesGatewayAgentList() throws {
        // Raw JSON array from GET /api/agents (gateway summaries, NOT wrapped
        // in an object).
        let json = #"""
        [
          {"id":"a1","name":"diane","flowType":"single","toolCount":20,"isDefault":false,"enabled":true,"createdAt":"2026-08-26T10:00:00Z","updatedAt":"2026-08-26T10:00:00Z"},
          {"id":"a2","name":"memory","flowType":"sequential","toolCount":3,"isDefault":true,"enabled":false,"createdAt":"2026-08-26T09:00:00Z","updatedAt":"2026-08-26T09:00:00Z"}
        ]
        """#
        let agents = try JSONDecoder().decode([Agent].self, from: Data(json.utf8))
        #expect(agents.count == 2)
        #expect(agents[0].id == "a1")
        #expect(agents[0].name == "diane")
        #expect(agents[0].flowType == "single")
        #expect(agents[0].toolCount == 20)
        #expect(agents[0].enabled == true)
        #expect(agents[1].isDefault == true)
        #expect(agents[1].enabled == false)
    }

    @Test func decodesDisabledAgentSummary() throws {
        // A disabled agent must decode with enabled == false.
        let json = #"""
        {"id":"a1","name":"diane","flowType":"single","toolCount":0,"isDefault":false,"enabled":false,"createdAt":"2026-08-26T10:00:00Z","updatedAt":"2026-08-26T10:01:00Z"}
        """#
        let agent = try JSONDecoder().decode(Agent.self, from: Data(json.utf8))
        #expect(agent.enabled == false)
    }
}
