import Foundation
import Testing
@testable import Memory

@Suite
@MainActor
struct AgentStoreTests {
    @Test func initReadsPersistedOrDefaultAgentName() {
        let suite = "AgentStoreTests.init.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite)!
        defer { defaults.removePersistentDomain(forName: suite) }

        let store = AgentStore(defaults: defaults)
        #expect(store.selectedAgentName == MemoryConfig.defaultAgentName)
    }

    @Test func selectPersistsAndPublishesName() throws {
        let suite = "AgentStoreTests.select.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite)!
        defer { defaults.removePersistentDomain(forName: suite) }

        let store = AgentStore(defaults: defaults)
        // Gateway agent list summary shape.
        let json = #"{"id":"1","name":"diane","flowType":"single","toolCount":20,"isDefault":false,"enabled":true}"#
        let agent = try JSONDecoder().decode(Agent.self, from: Data(json.utf8))

        store.select(agent)

        #expect(store.selectedAgentName == "diane")
        #expect(defaults.string(forKey: MemoryConfig.agentNameKey) == "diane")
    }

    @Test func decodeMissingFieldsDefensively() throws {
        // Summary shape: id/name may even be absent on a malformed record
        // without breaking the list.
        let summary = #"{"id":"1","name":"diane","flowType":"single","toolCount":20,"isDefault":false,"enabled":true}"#
        let agent = try JSONDecoder().decode(Agent.self, from: Data(summary.utf8))
        #expect(agent.id == "1")
        #expect(agent.name == "diane")
        #expect(agent.flowType == "single")
        #expect(agent.toolCount == 20)
        #expect(agent.isDefault == false)
        #expect(agent.enabled == true)

        let sparse = #"{}"#
        let empty = try JSONDecoder().decode(Agent.self, from: Data(sparse.utf8))
        #expect(empty.id == "")
        #expect(empty.name == "")
        #expect(empty.flowType == nil)
        // Missing `enabled` defaults to true so an odd record never breaks the
        // list or hides an agent from the picker.
        #expect(empty.enabled == true)
    }
}
