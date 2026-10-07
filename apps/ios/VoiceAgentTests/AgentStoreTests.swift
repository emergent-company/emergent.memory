import Foundation
@testable import Memory
import Testing

/// A recording agent-fetching fake. Returns a canned result and counts calls.
///
/// Main-actor isolated (the app builds with `-default-isolation=MainActor` and
/// ``AgentFetching`` is a main-actor protocol), so it is a class rather than an
/// actor, mirroring `FakeObjectClient`.
@MainActor
final class FakeAgentClient: AgentFetching {
    let result: Result<[Agent], Error>
    private(set) var callCount = 0

    init(result: Result<[Agent], Error>) {
        self.result = result
    }

    func listAgents() async throws -> [Agent] {
        callCount += 1
        return try result.get()
    }
}

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

    @Test func loadPublishesAgents() async throws {
        let suite = "AgentStoreTests.load.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }

        let json = #"{"id":"1","name":"diane","toolCount":20,"isDefault":false,"enabled":true}"#
        let agent = try JSONDecoder().decode(Agent.self, from: Data(json.utf8))
        let fake = FakeAgentClient(result: .success([agent]))
        let store = AgentStore(defaults: defaults, client: fake)

        await store.load()

        #expect(store.loadState == .loaded)
        #expect(store.agents.map(\.name) == ["diane"])
        #expect(fake.callCount == 1)
    }

    @Test func loadFailureSetsFailedState() async throws {
        struct Boom: Error {}
        let suite = "AgentStoreTests.loadFailure.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }

        let fake = FakeAgentClient(result: .failure(Boom()))
        let store = AgentStore(defaults: defaults, client: fake)

        await store.load()

        if case .failed = store.loadState {
            // expected
        } else {
            Issue.record("loadState = \(store.loadState), want .failed")
        }
        #expect(store.agents.isEmpty)
    }
}
