import Foundation
@testable import Memory
import Testing

@MainActor
struct AgentStoreTests {
    @Test func initReadsPersistedOrDefaultAgentName() throws {
        let suite = "AgentStoreTests.init.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }

        let store = AgentStore(defaults: defaults)
        #expect(store.selectedAgentName == MemoryConfig.defaultAgentName)
    }

    @Test func selectPersistsAndPublishesName() throws {
        let suite = "AgentStoreTests.select.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }

        let store = AgentStore(defaults: defaults)
        // Gateway agent list summary shape.
        let json = #"{"id":"1","name":"diane","toolCount":20,"isDefault":false,"enabled":true}"#
        let agent = try JSONDecoder().decode(Agent.self, from: Data(json.utf8))

        store.select(agent)

        #expect(store.selectedAgentName == "diane")
        #expect(defaults.string(forKey: MemoryConfig.agentNameKey) == "diane")
    }

    @Test func setActiveProjectLoadsPerProjectSelection() throws {
        let suite = "AgentStoreTests.perProject.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }

        defaults.set("agent-for-a", forKey: "\(MemoryConfig.agentNameKey).projA")
        let store = AgentStore(defaults: defaults)

        store.setActiveProject("projA")
        #expect(store.selectedAgentName == "agent-for-a")

        // A project with no stored pick falls back to the build default.
        store.setActiveProject("projB")
        #expect(store.selectedAgentName == MemoryConfig.defaultAgentName)

        // nil/empty falls back to the legacy single-agent key.
        defaults.set("legacy-agent", forKey: MemoryConfig.agentNameKey)
        store.setActiveProject(nil)
        #expect(store.selectedAgentName == "legacy-agent")
        store.setActiveProject("")
        #expect(store.selectedAgentName == "legacy-agent")
    }

    @Test func selectPersistsUnderActiveProjectKey() throws {
        let suite = "AgentStoreTests.selectPerProject.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }

        let store = AgentStore(defaults: defaults)
        store.setActiveProject("projA")

        let json = #"{"id":"1","name":"diane","toolCount":20,"isDefault":false,"enabled":true}"#
        let agent = try JSONDecoder().decode(Agent.self, from: Data(json.utf8))
        store.select(agent)

        #expect(defaults.string(forKey: "\(MemoryConfig.agentNameKey).projA") == "diane")
        // The legacy key is left untouched for the default (no-project) selection.
        #expect(defaults.string(forKey: MemoryConfig.agentNameKey) == nil)
    }

    @Test func decodeMissingFieldsDefensively() throws {
        // Summary shape: id/name may even be absent on a malformed record
        // without breaking the list.
        let summary = #"{"id":"1","name":"diane","toolCount":20,"isDefault":false,"enabled":true}"#
        let agent = try JSONDecoder().decode(Agent.self, from: Data(summary.utf8))
        #expect(agent.id == "1")
        #expect(agent.name == "diane")
        #expect(agent.toolCount == 20)
        #expect(agent.isDefault == false)
        #expect(agent.enabled == true)

        let sparse = #"{}"#
        let empty = try JSONDecoder().decode(Agent.self, from: Data(sparse.utf8))
        #expect(empty.id == "")
        #expect(empty.name == "")
        // Missing `enabled` defaults to true so an odd record never breaks the
        // list or hides an agent from the picker.
        #expect(empty.enabled == true)
    }
}
