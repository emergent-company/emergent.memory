import Foundation

/// Single decision point for whether the relay engine may run.
///
/// Policy: the engine runs **iff a project is connected** (persisted as
/// `ProjectProfile.connected`, surfaced as `ProjectStore.connectedProjectID`)
/// **and** the engine config file exists. This is what stops a stale
/// `~/.config/memory-connector.yml` (left over from a previously connected, now
/// disconnected project) from reconnecting at launch.
///
/// Pure and testable: callers pass the connected id + config URL, and may
/// inject a `FileManager` for deterministic tests.
enum EngineLifecyclePolicy {

    enum Decision: Equatable {
        /// A project is connected and its config exists → start/serve it.
        case run
        /// No project connected → the engine must stay stopped (regardless of
        /// any stale config file on disk).
        case noConnectedProject
        /// A project is connected but its config file is missing/invalid →
        /// surface an error; never silently start a stale config.
        case missingConfig(projectID: String)
    }

    /// The gate for "should the engine be running?".
    static func decision(connectedProjectID: String?,
                         configURL: URL,
                         fileManager: FileManager = .default) -> Decision {
        guard let projectID = connectedProjectID else { return .noConnectedProject }
        // "Usable" = the file exists AND parses as an engine config (has a
        // server_url). A missing/corrupt file for a connected project is an
        // error, never a reason to start a stale config.
        guard fileManager.fileExists(atPath: configURL.path),
              EngineConfigWriter.read(configURL: configURL) != nil else {
            return .missingConfig(projectID: projectID)
        }
        return .run
    }

    /// Convenience boolean for the same decision.
    static func shouldRun(connectedProjectID: String?,
                          configURL: URL,
                          fileManager: FileManager = .default) -> Bool {
        decision(connectedProjectID: connectedProjectID,
                 configURL: configURL,
                 fileManager: fileManager) == .run
    }
}
