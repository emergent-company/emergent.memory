import Foundation

/// Shared load state for the app's list/detail stores.
///
/// Replaces the four divergent per-surface definitions (agent list, object
/// browser, session list, session detail) with one shape. Emptiness is derived
/// from the loaded data — not represented as a state case — so the empty
/// session-detail case maps to `.loaded` with an empty record list (plus an
/// optional failure message) rather than a bespoke `.empty` case.
enum LoadState: Equatable, Sendable {
    case idle
    case loading
    case loaded
    case failed(String)
}
