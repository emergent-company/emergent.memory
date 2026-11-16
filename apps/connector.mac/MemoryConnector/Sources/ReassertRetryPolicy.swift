import Foundation

/// Retry policy for the launch/wake connection settle.
///
/// A transient failure (a timeout, a refused connection, a 503) must not
/// hammer the CLI or the API: the settle retries a short, fixed backoff and
/// then stops until the next trigger — a wake, a user action, or the next
/// launch. Anything else (settled, nothing connected, auth, permanent) ends the
/// settle immediately, because retrying cannot change the answer.
///
/// `sleep` is injected so a test asserts the number of attempts and the exact
/// delays without touching the clock or the wall time.
@MainActor
struct ReassertRetryPolicy {
    /// Delays BETWEEN attempts. Two delays means up to three attempts.
    let delays: [Duration]
    /// Awaited before the next attempt.
    let sleep: @MainActor (Duration) async -> Void

    /// Production policy: 2 s, then 8 s — about ten seconds of patience for a
    /// blip, short enough that a launch is never held hostage.
    static let standard = ReassertRetryPolicy(
        delays: [.seconds(2), .seconds(8)],
        sleep: { delay in try? await Task.sleep(for: delay) }
    )

    /// Attempts once, never retries (account switches, tests).
    static let none = ReassertRetryPolicy(delays: [], sleep: { _ in })

    /// Runs `attempt` until it stops reporting `.transientFailure`.
    func run(_ attempt: @MainActor () async -> ProjectStore.ReassertOutcome) async -> ProjectStore.ReassertOutcome {
        var outcome = await attempt()
        for delay in delays where outcome == .transientFailure {
            await sleep(delay)
            outcome = await attempt()
        }
        return outcome
    }
}
