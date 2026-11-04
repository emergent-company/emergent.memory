import Foundation

/// A monotonic load-generation counter shared by the stores.
///
/// `next()` returns a token valid only while it is still the latest
/// generation; `isCurrent(_:)` discards results from a superseded load so a
/// slow earlier request can never clobber a newer one. Extracted verbatim from
/// the per-store `generation` counters in `AgentStore` and
/// `ObjectBrowserStore`, so the cancellation semantics are unchanged.
struct GenerationGuard: Sendable {
    private(set) var generation = 0

    /// The latest generation (read without incrementing, for pagination that
    /// must detect a superseding full load).
    var current: Int {
        generation
    }

    /// Increments and returns the new generation token.
    mutating func next() -> Int {
        generation += 1
        return generation
    }

    /// True when `token` is still the latest generation.
    func isCurrent(_ token: Int) -> Bool {
        token == generation
    }
}
