import Foundation

/// Depth-counter gate: while an account-scope swap is in flight, the
/// connected-project-id sink must not reconcile — a swap's id transitions
/// (nil → restored value) would each start the engine against the stale config.
/// A counter — not a boolean — keeps overlapping swaps suppressed until all of
/// them have settled.
///
/// Incremented synchronously on the `@MainActor` thread *before* `applyScope`
/// mutates the id; decremented only after the config rewrite and the single
/// reconcile. Pure and unit-testable.
struct ScopeSwapGate {
    private(set) var depth = 0

    /// Begins a scope swap. Reconciliation stays suppressed while `depth > 0`.
    mutating func begin() {
        depth += 1
    }

    /// True when the sink may reconcile (no swap in flight).
    var mayReconcile: Bool { depth == 0 }

    /// Ends one scope swap. Clamped at zero so an unbalanced end can never
    /// reopen reconciliation while a real swap is still in flight.
    mutating func end() {
        if depth > 0 { depth -= 1 }
    }
}
