import Foundation
@testable import Memory
import Testing

/// A recording project-config fake: returns a mutable canned result and counts
/// calls, so a test can fail a later refresh without rebuilding the store.
///
/// Main-actor isolated (the app builds with `-default-isolation=MainActor`),
/// mirroring `FakeAgentClient`.
@MainActor
final class FakeProjectConfigClient: ProjectConfigFetching {
    var result: Result<ProjectConfig, Error>
    private(set) var callCount = 0

    init(result: Result<ProjectConfig, Error>) {
        self.result = result
    }

    func fetch() async throws -> ProjectConfig {
        callCount += 1
        return try result.get()
    }
}

@MainActor
struct ProjectConfigStoreTests {
    private struct Boom: Error {}

    private func makeDefaults() throws -> (UserDefaults, String) {
        let suite = "ProjectConfigStoreTests.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        return (defaults, suite)
    }

    /// Nothing known yet (first launch, no cached value): voice is offered.
    /// Hiding a working call affordance on a failed probe would be worse than
    /// showing one the server rejects.
    @Test func unknownProjectDefaultsToEnabled() throws {
        let (defaults, suite) = try makeDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }

        let store = ProjectConfigStore(defaults: defaults)
        #expect(store.voiceEnabled)
    }

    @Test func refreshPublishesFetchedValueAndCachesIt() async throws {
        let (defaults, suite) = try makeDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }

        let fake = FakeProjectConfigClient(result: .success(ProjectConfig(voiceEnabled: false)))
        let store = ProjectConfigStore(defaults: defaults, client: fake)

        await store.refresh(projectID: "projA")

        #expect(fake.callCount == 1)
        #expect(!store.voiceEnabled)
        #expect(defaults.bool(forKey: "alfred.voiceEnabled.projA") == false)
    }

    /// A fresh launch publishes the cached answer even before the network
    /// answers — and keeps it when the fetch fails.
    @Test func cachedValueIsUsedWhenTheFetchFails() async throws {
        let (defaults, suite) = try makeDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        defaults.set(false, forKey: "alfred.voiceEnabled.projA")

        let fake = FakeProjectConfigClient(result: .failure(Boom()))
        let store = ProjectConfigStore(defaults: defaults, client: fake)

        await store.refresh(projectID: "projA")

        #expect(fake.callCount == 1)
        #expect(!store.voiceEnabled, "the cached voice-off must survive a failed fetch")
    }

    @Test func enabledProjectStaysEnabled() async throws {
        let (defaults, suite) = try makeDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }

        let fake = FakeProjectConfigClient(result: .success(ProjectConfig(voiceEnabled: true)))
        let store = ProjectConfigStore(defaults: defaults, client: fake)

        await store.refresh(projectID: "projA")

        #expect(store.voiceEnabled)
        #expect(defaults.bool(forKey: "alfred.voiceEnabled.projA"))
    }

    /// A failed probe is not evidence that voice is off: keep the last known
    /// value (and the cache) instead of flipping the UI.
    @Test func failedRefreshKeepsLastKnownValue() async throws {
        let (defaults, suite) = try makeDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }

        let fake = FakeProjectConfigClient(result: .success(ProjectConfig(voiceEnabled: false)))
        let store = ProjectConfigStore(defaults: defaults, client: fake)
        await store.refresh(projectID: "projA")
        #expect(!store.voiceEnabled)

        fake.result = .failure(Boom())
        await store.refresh(projectID: "projA")

        #expect(fake.callCount == 2)
        #expect(!store.voiceEnabled, "a failed probe must not enable voice")
        #expect(defaults.bool(forKey: "alfred.voiceEnabled.projA") == false)
    }

    /// Switching projects publishes that project's own cached answer before the
    /// fetch, so the call affordance never flashes the previous project's
    /// setting (and stays right if the fetch then fails).
    @Test func switchingProjectUsesThatProjectsCache() async throws {
        let (defaults, suite) = try makeDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }

        let fake = FakeProjectConfigClient(result: .success(ProjectConfig(voiceEnabled: true)))
        let store = ProjectConfigStore(defaults: defaults, client: fake)
        await store.refresh(projectID: "projA")
        #expect(store.voiceEnabled)

        // projB was seen earlier with voice off.
        defaults.set(false, forKey: "alfred.voiceEnabled.projB")
        fake.result = .failure(Boom())

        await store.refresh(projectID: "projB")

        #expect(!store.voiceEnabled, "projB's cached voice-off must survive a failed refresh")

        // Back to projA: its cached value is on again.
        await store.refresh(projectID: "projA")
        #expect(store.voiceEnabled)
    }
}
