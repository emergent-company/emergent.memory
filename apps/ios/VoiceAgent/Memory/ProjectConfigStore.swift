import Foundation
import Observation

/// Caches the active project's voice enablement.
///
/// The app offers voice (Start call, audio options) only when the project has
/// it enabled, and the project setting lives server-side. This store keeps the
/// answer in sync: it remembers the last known value **per project** so a cold
/// launch renders the right affordance before the network answers, and it
/// refreshes on launch, on project switch, on `.memoryConfigChanged` and when
/// the app returns to the foreground.
///
/// Unknown means **enabled**: a failed or not-yet-completed probe must never
/// hide a working call affordance, and the server still enforces the setting.
@MainActor
@Observable
final class ProjectConfigStore {
    /// Whether the active project offers voice. Defaults to `true` (unknown).
    private(set) var voiceEnabled = true

    /// Bumped on every refresh; a stale response cannot overwrite a newer one
    /// (a slow fetch for the previous project must not decide the next one's
    /// affordance).
    @ObservationIgnored private var loadGeneration = GenerationGuard()

    @ObservationIgnored private let defaults: UserDefaults
    /// Injected fetching seam; nil builds a fresh live client per refresh, so a
    /// re-onboarded or rotated credential is picked up without a relaunch.
    @ObservationIgnored private let client: (any ProjectConfigFetching)?
    /// Project whose cached value is currently published (nil = no project).
    @ObservationIgnored private var activeProjectID: String?

    init(defaults: UserDefaults = .standard, client: (any ProjectConfigFetching)? = nil) {
        self.defaults = defaults
        self.client = client
    }

    /// Publishes the cached value for `projectID` (immediately) and then
    /// refreshes it from the gateway.
    ///
    /// Switching projects republishes that project's cached value before the
    /// fetch so the call affordance never flashes the previous project's answer.
    func refresh(projectID: String?) async {
        let normalized = (projectID?.isEmpty == false) ? projectID : nil
        if normalized != activeProjectID {
            activeProjectID = normalized
            voiceEnabled = cached(for: normalized) ?? true
        }

        let generation = loadGeneration.next()
        let client = client ?? ProjectConfigClient(config: MemoryConfig())
        do {
            let fetched = try await client.fetch()
            guard loadGeneration.isCurrent(generation) else { return }
            voiceEnabled = fetched.voiceEnabled
            if let normalized {
                defaults.set(fetched.voiceEnabled, forKey: Self.cacheKey(normalized))
            }
            Log.net.info("project config voiceEnabled=\(fetched.voiceEnabled)")
        } catch {
            guard loadGeneration.isCurrent(generation) else { return }
            // Keep the cached/last known value: a failed probe is not evidence
            // that voice is off.
            Log.net.error("project config fetch failed: \(error.localizedDescription)")
        }
    }

    // MARK: - Cache

    /// Per-project cache key. A build-config default keeps the app readable
    /// before the first successful fetch of a freshly onboarded project.
    private static func cacheKey(_ projectID: String) -> String {
        "alfred.voiceEnabled.\(projectID)"
    }

    private func cached(for projectID: String?) -> Bool? {
        guard let projectID else { return nil }
        let key = Self.cacheKey(projectID)
        guard defaults.object(forKey: key) != nil else { return nil }
        return defaults.bool(forKey: key)
    }
}
