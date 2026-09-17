import Foundation

/// Single decision point for whether the relay engine may run.
///
/// Policy: the engine runs **iff a project is connected** (persisted as
/// `ProjectProfile.connected`, surfaced as `ProjectStore.connectedProjectID`),
/// its engine config file exists, AND that config binds **that** project (and,
/// when an expected server is supplied, that server). This is what stops a
/// stale `~/.config/memory-connector.yml` (left over from a previously
/// connected, now disconnected project) from reconnecting at launch — and stops
/// the engine from serving another account's/ecosystem's config.
///
/// Pure and testable: callers pass the connected id + config URL (+ optional
/// expected server), and may inject a `FileManager` for deterministic tests.
enum EngineLifecyclePolicy {

    enum Decision: Equatable {
        /// A project is connected and its config exists and binds that project
        /// (and server, when known) → start/serve it.
        case run
        /// No project connected → the engine must stay stopped (regardless of
        /// any stale config file on disk).
        case noConnectedProject
        /// A project is connected but its config file is missing/invalid →
        /// surface an error; never silently start a stale config.
        case missingConfig(projectID: String)
        /// The config on disk binds a different project than the connected one
        /// (or an empty one) — or, when an expected server is known, a
        /// different server. `configuredProjectID` is the id the config binds
        /// (`nil` when absent/empty). A configuration error, never a start.
        case wrongProject(projectID: String, configuredProjectID: String?)
    }

    /// The gate for "should the engine be running?".
    ///
    /// - Parameters:
    ///   - expectedServerURL: the server the config must bind, when known. A
    ///     `nil`/blank value skips the server comparison (defaults to `nil`,
    ///     preserving the previous behaviour for callers that don't supply one).
    static func decision(connectedProjectID: String?,
                         configURL: URL,
                         expectedServerURL: String? = nil,
                         fileManager: FileManager = .default) -> Decision {
        guard let projectID = connectedProjectID else { return .noConnectedProject }
        // "Usable" = the file exists AND parses as an engine config (has a
        // server_url). A missing/corrupt file for a connected project is an
        // error, never a reason to start a stale config.
        guard fileManager.fileExists(atPath: configURL.path),
              let values = EngineConfigWriter.read(configURL: configURL) else {
            return .missingConfig(projectID: projectID)
        }

        // The config must bind the connected project. An empty `project_id`
        // (or a different one) is a configuration error, never a reason to
        // serve it.
        let configuredProjectID: String? = values.projectID.isEmpty ? nil : values.projectID
        if configuredProjectID != projectID {
            return .wrongProject(projectID: projectID, configuredProjectID: configuredProjectID)
        }

        // When the expected server is known, the config must bind it too.
        if let expected = expectedServerURL?.trimmingCharacters(in: .whitespacesAndNewlines),
           !expected.isEmpty {
            let configuredServer = values.serverURL.trimmingCharacters(in: .whitespacesAndNewlines)
            if configuredServer != expected {
                return .wrongProject(projectID: projectID, configuredProjectID: configuredProjectID)
            }
        }

        return .run
    }

    /// Convenience boolean for the same decision.
    static func shouldRun(connectedProjectID: String?,
                          configURL: URL,
                          expectedServerURL: String? = nil,
                          fileManager: FileManager = .default) -> Bool {
        decision(connectedProjectID: connectedProjectID,
                 configURL: configURL,
                 expectedServerURL: expectedServerURL,
                 fileManager: fileManager) == .run
    }
}
