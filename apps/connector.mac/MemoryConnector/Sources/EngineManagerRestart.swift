import Foundation

/// Pure restart-supervision decisions for ``EngineManager``, split out of the
/// supervisor's main body so the lifecycle file stays within the lint length
/// limits. Nothing here touches instance state.
extension EngineManager {
    /// The action `restart()` should take. Extracted so the sequencing rule is
    /// pure and unit-testable without spawning a process.
    enum RestartAction: Equatable {
        /// No live child and no transition in flight: start directly.
        case startNow
        /// A live child must exit before its successor is spawned.
        case terminateThenRestart
        /// A spawn/termination is already in flight: queue the restart.
        case queueRestart
    }

    /// How long to wait for a SIGTERM'd child to exit before killing it, so a
    /// queued restart can never be wedged by a hung child.
    nonisolated static let terminateGracePeriod: TimeInterval = 5

    /// The pure restart-sequencing decision.
    ///
    /// - `hasLiveProcess`: a child process is currently running.
    /// - `isBusy`: the manager is mid spawn/termination (`.starting`/`.restarting`)
    ///   but has no running child to wait on.
    static func restartAction(hasLiveProcess: Bool, isBusy: Bool) -> RestartAction {
        if hasLiveProcess {
            return .terminateThenRestart
        }
        if isBusy {
            return .queueRestart
        }
        return .startNow
    }

    /// The engine command line. Extracted so the argument list is testable
    /// without spawning a process.
    nonisolated static func launchArguments(configPath: String) -> [String] {
        ["relay", "--config", configPath, "--api-port", String(ManagementPortProbe.port)]
    }
}
