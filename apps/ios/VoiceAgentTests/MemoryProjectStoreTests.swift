import Combine
import Foundation
@testable import Memory
import Testing

@MainActor
struct MemoryProjectStoreTests {
    /// Per-test storage: an isolated defaults suite (never real app storage)
    /// plus an isolated `NotificationCenter`. Posting `.memoryConfigChanged`
    /// to `.default` would reach the running app host's observer and build a
    /// LiveKit session, which deadlocks under test.
    private struct Harness {
        let defaults: UserDefaults
        let suite: String
        let center: NotificationCenter

        var store: MemoryProjectStore {
            MemoryProjectStore(defaults: defaults, notificationCenter: center)
        }
    }

    private func makeHarness() throws -> Harness {
        let suite = "MemoryProjectStoreTests.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        return Harness(defaults: defaults, suite: suite, center: NotificationCenter())
    }

    private func connection(
        _ projectID: String,
        name: String? = nil,
        apiKey: String? = nil
    ) -> MemoryProjectConnection {
        MemoryProjectConnection(
            projectID: projectID,
            orgID: "org-\(projectID)",
            projectName: name ?? "Project \(projectID.uppercased())",
            serverURL: "ws://\(projectID).example:7880",
            tokenEndpoint: "http://\(projectID).example:8080/api/token",
            apiBaseURL: "http://\(projectID).example:8080",
            apiKey: apiKey ?? "key-\(projectID)",
            ttsStrategy: "server"
        )
    }

    @Test func roundTripRestoresConnectionsAndActive() throws {
        let harness = try makeHarness()
        let defaults = harness.defaults
        let center = harness.center
        defer { defaults.removePersistentDomain(forName: harness.suite) }

        let store = harness.store
        store.upsert(connection("a"))
        store.upsert(connection("b"))
        store.setActive(projectID: "b")

        let reloaded = harness.store
        #expect(reloaded.connections.map(\.projectID).sorted() == ["a", "b"])
        #expect(reloaded.activeProjectID == "b")
        #expect(reloaded.activeConnection?.apiKey == "key-b")
    }

    @Test func upsertReplacesByProjectInsteadOfDuplicating() throws {
        let harness = try makeHarness()
        let defaults = harness.defaults
        let center = harness.center
        defer { defaults.removePersistentDomain(forName: harness.suite) }

        let store = harness.store
        store.upsert(connection("a"))

        var updated = connection("a")
        updated.serverURL = "ws://a.example:9999"
        updated.apiKey = "key-rotated"
        store.upsert(updated)

        #expect(store.connections.count == 1)
        #expect(store.connections[0].serverURL == "ws://a.example:9999")
        #expect(store.connections[0].apiKey == "key-rotated")
    }

    @Test func upsertFillsInLegacyIdentityPlaceholder() throws {
        let harness = try makeHarness()
        let defaults = harness.defaults
        let center = harness.center
        defer { defaults.removePersistentDomain(forName: harness.suite) }

        defaults.set("legacy-key", forKey: MemoryConfig.apiKeyKey)
        let store = harness.store
        #expect(store.connections.count == 1)
        #expect(store.connections[0].hasIdentity == false)

        store.upsert(connection("a"))
        #expect(store.connections.count == 1)
        #expect(store.connections[0].projectID == "a")
    }

    @Test func setActiveMirrorsConfigAndPostsNotification() throws {
        let harness = try makeHarness()
        let defaults = harness.defaults
        let center = harness.center
        defer { defaults.removePersistentDomain(forName: harness.suite) }

        let store = harness.store
        store.upsert(connection("a"))
        store.upsert(connection("b"))
        #expect(store.activeProjectID == "a")

        var notifications = 0
        let token = center.publisher(for: .memoryConfigChanged).sink { _ in
            notifications += 1
        }
        defer { token.cancel() }

        store.setActive(projectID: "b")

        #expect(notifications == 1)
        let config = MemoryConfig(defaults: defaults)
        #expect(config.apiBaseURL == "http://b.example:8080")
        #expect(config.tokenEndpoint == "http://b.example:8080/api/token")
        #expect(config.serverURL == "ws://b.example:7880")
        #expect(config.apiKey == "key-b")
        #expect(config.projectID == "b")
        #expect(config.orgID == "org-b")
        #expect(config.projectName == "Project B")
    }

    @Test func removeActiveConnectionActivatesNext() throws {
        let harness = try makeHarness()
        let defaults = harness.defaults
        let center = harness.center
        defer { defaults.removePersistentDomain(forName: harness.suite) }

        let store = harness.store
        store.upsert(connection("a"))
        store.upsert(connection("b"))
        store.setActive(projectID: "a")

        store.remove(projectID: "a")

        #expect(store.connections.map(\.projectID) == ["b"])
        #expect(store.activeProjectID == "b")
        #expect(MemoryConfig(defaults: defaults).apiKey == "key-b")

        store.remove(projectID: "b")
        #expect(store.connections.isEmpty)
        #expect(store.activeProjectID == nil)
        #expect(store.activeConnection == nil)
        // No connection means no credential: the mirror must drop the removed
        // project's device token instead of leaving every client pointed at a
        // server the user disconnected from.
        #expect(MemoryConfig(defaults: defaults).apiKey.isEmpty)
    }

    @Test func legacyMigrationSynthesizesAndActivatesConnection() throws {
        let harness = try makeHarness()
        let defaults = harness.defaults
        let center = harness.center
        defer { defaults.removePersistentDomain(forName: harness.suite) }

        // A pre-multi-project install: credential present, no project list.
        defaults.set("legacy-key", forKey: MemoryConfig.apiKeyKey)
        defaults.set("http://legacy.example:1234", forKey: MemoryConfig.apiBaseURLKey)
        #expect(defaults.object(forKey: MemoryProjectStore.projectsKey) == nil)

        let store = harness.store

        #expect(store.connections.count == 1)
        #expect(store.connections[0].apiKey == "legacy-key")
        #expect(store.connections[0].apiBaseURL == "http://legacy.example:1234")
        #expect(store.connections[0].hasIdentity == false)
        #expect(store.activeProjectID == "")
        #expect(store.activeConnection?.apiKey == "legacy-key")
        // Migration persists the synthesized list so it only runs once.
        #expect(defaults.object(forKey: MemoryProjectStore.projectsKey) != nil)
    }

    @Test func noMigrationWhenProjectsListExists() throws {
        let harness = try makeHarness()
        let defaults = harness.defaults
        let center = harness.center
        defer { defaults.removePersistentDomain(forName: harness.suite) }

        defaults.set("legacy-key", forKey: MemoryConfig.apiKeyKey)
        defaults.set(Data("[]".utf8), forKey: MemoryProjectStore.projectsKey)
        let store = harness.store

        #expect(store.connections.isEmpty)
        #expect(store.activeProjectID == nil)
    }

    @Test func connectionDecodesTolerantly() throws {
        let json = #"{"serverURL":"ws://x","tokenEndpoint":"http://t","apiBaseURL":"http://a","apiKey":"k"}"#
        let decoded = try JSONDecoder().decode(MemoryProjectConnection.self, from: Data(json.utf8))

        #expect(decoded.projectID == "")
        #expect(decoded.orgID == "")
        #expect(decoded.projectName == "")
        #expect(decoded.ttsStrategy == "server")
        #expect(decoded.displayName == "Memory")
        #expect(decoded.hasIdentity == false)
    }

    @Test func displayNameFallsBackToProjectID() {
        #expect(connection("p", name: "").displayName == "p")
        #expect(connection("p", name: "Readable").displayName == "Readable")
    }
}
