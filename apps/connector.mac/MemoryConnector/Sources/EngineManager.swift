import Foundation
import Observation

/// Supervises the embedded `memory-connector` engine processes — one child per
/// connected project.
///
/// The engine binary ships inside the app bundle (Contents/Resources, built by
/// the `Build embedded engine binary` run-script phase) and is spawned ONLY as
/// a direct `Process` child — no launchd, no NSWorkspace — so Automation (TCC)
/// grants stay attributed to this app. Each child (`ChildSupervisor`) binds one
/// project's config file and loopback management port; a child's failure is
/// isolated to that project (its crash / token / port / relay outage never
/// stops the other projects' children).
@MainActor
@Observable
final class EngineManager {
    /// Process lifecycle for the menu-bar UI (aggregate across children).
    enum State: Equatable {
        case stopped
        case starting
        case running
        case restarting
        case failed
    }

    /// Shared instance owned by the app; the app delegate stops it on quit.
    static let shared = EngineManager()

    /// Legacy config path for the FIRST/primary connected project:
    /// `relay --config ~/.config/memory-connector.yml`.
    nonisolated static let defaultConfigPath = NSHomeDirectory() + "/.config/memory-connector.yml"

    /// Shared app log at `~/Library/Logs/memory-connector-app.log` (primary
    /// project's child logs here, so a single-project user sees no change).
    nonisolated static let logFileURL = URL(fileURLWithPath: NSHomeDirectory())
        .appendingPathComponent("Library/Logs/memory-connector-app.log")

    /// Log file for a child: the shared app log for the primary project, a
    /// per-project file otherwise, so each child's output stays attributable.
    nonisolated static func logFileURL(for projectID: String, port: Int) -> URL {
        guard port != ManagementPortProbe.primaryPort else { return logFileURL }
        return URL(fileURLWithPath: NSHomeDirectory())
            .appendingPathComponent("Library/Logs/memory-connector-app-\(projectID).log")
    }

    /// One `ChildSupervisor` per connected project, keyed by project id.
    private(set) var children: [String: ChildSupervisor] = [:]

    init() {}

    // MARK: - Aggregate state

    /// The aggregate lifecycle state across all children: running if any child
    /// is running, else the most severe of the remaining states.
    var state: State {
        if children.isEmpty {
            return .stopped
        }
        if children.values.contains(where: { $0.state == .running }) {
            return .running
        }
        if children.values.contains(where: { $0.state == .restarting }) {
            return .restarting
        }
        if children.values.contains(where: { $0.state == .starting }) {
            return .starting
        }
        if children.values.contains(where: { $0.state == .failed }) {
            return .failed
        }
        return .stopped
    }

    // MARK: - Child lifecycle

    /// Ensures a child exists for `projectID` and starts it (reconcile path —
    /// no circuit-breaker reset). Safe to call repeatedly.
    func start(projectID: String, configPath: String, port: Int) {
        child(for: projectID, configPath: configPath, port: port).start()
    }

    /// Ensures a child exists for `projectID` and restarts it with `configPath`
    /// and `port` (a genuine configuration change — resets the circuit breaker).
    func restart(projectID: String, configPath: String, port: Int) {
        child(for: projectID, configPath: configPath, port: port).restart()
    }

    /// Stops and removes the child for `projectID` (its project was
    /// disconnected). Idempotent; other children are untouched.
    func stop(projectID: String) {
        children[projectID]?.stop()
        children.removeValue(forKey: projectID)
    }

    /// Stops and removes every child (scope teardown / quit).
    func stopAll() {
        for child in children.values {
            child.stop()
        }
        children.removeAll()
    }

    /// Surfaces a configuration error for `projectID`'s child without spawning
    /// anything (a missing/mismatched config must never start a stale process).
    func reportConfigurationError(_ message: String,
                                  for projectID: String,
                                  configPath: String,
                                  port: Int)
    {
        child(for: projectID, configPath: configPath, port: port)
            .reportConfigurationError(message)
    }

    /// True when `projectID` has a running child.
    func isRunning(projectID: String) -> Bool {
        children[projectID]?.state == .running
    }

    /// The lifecycle state of `projectID`'s child (`.stopped` when none exists).
    func state(for projectID: String) -> State {
        children[projectID]?.state ?? .stopped
    }

    /// The management port bound by `projectID`'s child, when one exists.
    func managementPort(for projectID: String) -> Int? {
        children[projectID]?.port
    }

    // MARK: - Child bookkeeping

    /// Returns the child for `projectID`, creating one bound to `configPath` +
    /// `port` when none exists (or when the existing child's binding changed).
    private func child(for projectID: String, configPath: String, port: Int) -> ChildSupervisor {
        if let existing = children[projectID] {
            if existing.configPath == configPath, existing.port == port {
                return existing
            }
            existing.stop()
            children.removeValue(forKey: projectID)
        }
        let child = ChildSupervisor(projectID: projectID,
                                    configPath: configPath,
                                    port: port,
                                    logFileURL: Self.logFileURL(for: projectID, port: port))
        children[projectID] = child
        return child
    }
}
