import Foundation

/// Runs at most one instance of an async operation at a time.
///
/// The launch/wake settle has several triggers that can fire at once — the app
/// delegate and the main window's `.task` on a cold launch, and a system wake
/// during either — and a second run would settle the same connections twice,
/// doubling the CLI calls and the retry count. A caller that arrives while a run
/// is in flight joins it and receives the same result.
///
/// The guard is deliberately **not** "once per process": the run is cleared when
/// it finishes, so the next trigger (a later wake, or the window being reopened)
/// runs the work again. A guard that stayed set would silently stop refreshing
/// data after the first launch.
@MainActor
final class SingleFlight<Value: Sendable> {
    private var task: Task<Value, Never>?

    /// True while a run is in flight.
    var isRunning: Bool {
        task != nil
    }

    /// Awaits the in-flight run, if there is one; returns immediately otherwise.
    func join() async {
        guard let task else { return }
        _ = await task.value
    }

    /// Runs `work`, or joins the run already in flight and returns its value.
    @discardableResult
    func run(_ work: @escaping @MainActor () async -> Value) async -> Value {
        if let inFlight = task {
            return await inFlight.value
        }
        let run = Task { @MainActor in
            let value = await work()
            // Clear the slot inside the run, BEFORE the task completes: every
            // waiter (this caller, or a `join()`) resumes on the finished task,
            // so clearing afterwards left a window in which a finished run still
            // read as in flight and a wake that arrived then was dropped.
            self.task = nil
            return value
        }
        task = run
        return await run.value
    }
}
