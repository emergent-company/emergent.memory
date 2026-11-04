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

/// A fake whose `fetch()` suspends until the test resumes it, so two refreshes
/// can be answered out of order and the stale one proven dropped.
@MainActor
final class SuspendingProjectConfigClient: ProjectConfigFetching {
    private var pending: [CheckedContinuation<ProjectConfig, Error>] = []
    private(set) var callCount = 0

    func fetch() async throws -> ProjectConfig {
        callCount += 1
        return try await withCheckedThrowingContinuation { pending.append($0) }
    }

    /// Yields until `count` fetches are suspended (bounded, so a broken seam
    /// fails the expectation instead of hanging the suite).
    func waitForPending(_ count: Int) async {
        for _ in 0 ..< 500 where pending.count < count {
            await Task.yield()
        }
    }

    func resume(_ index: Int, with result: Result<ProjectConfig, Error>) {
        pending[index].resume(with: result)
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

    /// A project the app has never seen has no cache entry. Switching to it
    /// after a voice-disabled project must fall back to unknown = enabled, not
    /// inherit the previous project's answer.
    @Test func switchingToAnUncachedProjectDoesNotInheritVoiceOff() async throws {
        let (defaults, suite) = try makeDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        defaults.set(false, forKey: "alfred.voiceEnabled.projA")

        let fake = FakeProjectConfigClient(result: .failure(Boom()))
        let store = ProjectConfigStore(defaults: defaults, client: fake)

        await store.refresh(projectID: "projA")
        #expect(!store.voiceEnabled)

        // projB is uncached and its probe fails: voice is offered.
        await store.refresh(projectID: "projB")
        #expect(store.voiceEnabled, "an uncached project must not inherit the previous project's voice-off")
        #expect(defaults.object(forKey: "alfred.voiceEnabled.projB") == nil, "no cache is written without a successful fetch")
    }

    /// A slow answer for the project the user just left must be discarded — both
    /// the published value and the cache write — because it would otherwise
    /// decide the *current* project's affordance and mis-cache the old one.
    @Test func staleResponseForThePreviousProjectIsDropped() async throws {
        let (defaults, suite) = try makeDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }

        let fake = SuspendingProjectConfigClient()
        let store = ProjectConfigStore(defaults: defaults, client: fake)

        // projA's refresh starts and suspends inside fetch().
        let slow = Task { await store.refresh(projectID: "projA") }
        await fake.waitForPending(1)
        #expect(fake.callCount == 1)

        // The user switches to projB before projA answers.
        let fast = Task { await store.refresh(projectID: "projB") }
        await fake.waitForPending(2)

        // projB answers first: voice on.
        fake.resume(1, with: .success(ProjectConfig(voiceEnabled: true)))
        await fast.value
        #expect(store.voiceEnabled)

        // projA's late answer says off; the generation guard must drop it.
        fake.resume(0, with: .success(ProjectConfig(voiceEnabled: false)))
        await slow.value

        #expect(store.voiceEnabled, "a stale answer for the previous project must not decide the current one")
        #expect(defaults.object(forKey: "alfred.voiceEnabled.projA") == nil, "a stale answer must not be cached")
        #expect(defaults.bool(forKey: "alfred.voiceEnabled.projB") == true)
    }
}
