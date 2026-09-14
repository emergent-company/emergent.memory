import Foundation

/// Outcome of feeding one engine exit into a `RestartPolicy`.
enum RestartDecision: Equatable {
    /// Restart the engine after the given delay.
    case restart(after: TimeInterval)
    /// Too many exits inside the window — stop auto-restarting.
    case giveUp
}

/// Pure circuit-breaker bookkeeping for the engine process. Deterministic and
/// unit-testable: callers inject `now` via `registerExit(at:)` and no clocks
/// or processes are touched here.
struct RestartPolicy: Equatable {
    let maxRestarts: Int
    let window: TimeInterval
    let restartDelay: TimeInterval

    private(set) var restartCount = 0
    private var windowStart: Date?

    init(maxRestarts: Int = 3, window: TimeInterval = 60, restartDelay: TimeInterval = 3) {
        self.maxRestarts = maxRestarts
        self.window = window
        self.restartDelay = restartDelay
    }

    /// Records one process exit. Returns `.restart` while the exit count is
    /// within `maxRestarts` inside `window`; returns `.giveUp` once the
    /// window is exhausted. A gap longer than `window` between exits resets
    /// the count.
    mutating func registerExit(at now: Date = Date()) -> RestartDecision {
        if let start = windowStart {
            if now.timeIntervalSince(start) > window {
                restartCount = 0
                windowStart = now
            }
        } else {
            windowStart = now
        }
        restartCount += 1
        if restartCount > maxRestarts {
            return .giveUp
        }
        return .restart(after: restartDelay)
    }
}
