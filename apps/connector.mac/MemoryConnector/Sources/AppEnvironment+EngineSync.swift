import Foundation

/// Engine lifecycle gating: reconciles the running engine children with the
/// connected-project set.
///
/// Split from `AppEnvironment.swift` so neither file outgrows the SwiftLint
/// file/type limits. It only reads the environment's stores, so it needs no
/// private state of its own.
extension AppEnvironment {
    /// Reconciles the engine children with the connected-project set:
    ///
    /// - connected + config exists and binds that project (and the expected
    ///   server, when known) → ensure that project's child is running;
    /// - no connected projects → stop every child (a stale config must never
    ///   reconnect);
    /// - connected but config missing → stop that child and surface an error
    ///   (never start a stale/other project's config);
    /// - config binds a different project/server → stop that child and surface a
    ///   configuration error naming both project ids (never start it).
    ///
    /// A mismatch in ONE project's config stops only that project's child; every
    /// other project keeps running. Safe to call repeatedly (`start()` no-ops
    /// while running, `stop()` is idempotent).
    func syncEngineWithConnections() {
        let expectedServer = accountStore.activeEnvironment?.serverURLString ?? settings.serverURL
        let expectedServerTrimmed = expectedServer.trimmingCharacters(in: .whitespacesAndNewlines)
        let desired = projectStore.connectedProjectIDs

        // Stop children for projects no longer connected.
        for projectID in engine.children.keys where !desired.contains(projectID) {
            engine.stop(projectID: projectID)
        }

        for projectID in desired.sorted() {
            let target = projectStore.engineTarget(for: projectID)
            let decision = EngineLifecyclePolicy.decision(
                connectedProjectID: projectID,
                configURL: URL(fileURLWithPath: target.configPath),
                expectedServerURL: expectedServerTrimmed.isEmpty ? nil : expectedServerTrimmed
            )
            switch decision {
            case .run:
                engine.start(projectID: projectID, configPath: target.configPath, port: target.port)
            case .noConnectedProject:
                engine.stop(projectID: projectID)
            case let .missingConfig(projectID):
                engine.stop(projectID: projectID)
                engine.reportConfigurationError(
                    "Engine config for connected project \(projectID) is missing; not starting.",
                    for: projectID, configPath: target.configPath, port: target.port
                )
            case let .wrongProject(projectID, configured):
                engine.stop(projectID: projectID)
                // The decision carries no mismatch kind, so distinguish by shape:
                // a nil configured id means the config binds no project, and a
                // configured id equal to the connected one can only be a server
                // mismatch (the project itself matched).
                let reason = switch configured {
                case .none:
                    "the engine config binds no project"
                case let .some(configuredID) where configuredID == projectID:
                    "the engine config binds a different server for project \(projectID)"
                case let .some(configuredID):
                    "the engine config binds project \(configuredID)"
                }
                engine.reportConfigurationError(
                    "Engine config does not match the connected project \(projectID): \(reason); not starting.",
                    for: projectID, configPath: target.configPath, port: target.port
                )
            }
        }

        // Status polling follows the connected set: poll while any project is
        // connected, publish "not running" when none is.
        if desired.isEmpty {
            statusMonitor.stop()
            statusMonitor.markStopped()
        } else {
            statusMonitor.start()
        }
    }
}
