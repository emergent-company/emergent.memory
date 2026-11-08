import Foundation
import Observation

/// Loads one object card and, on demand, its version history.
///
/// The history is a second request the card only makes when its History
/// section first appears, as the web drawer lazy-loads its history partial.
/// A history failure never takes the card down with it.
@MainActor
@Observable
final class ObjectCardStore {
    let reference: ObjectReference

    private(set) var card: ObjectCard?
    private(set) var loadState: LoadState = .idle
    private(set) var history: [ObjectCardVersion] = []
    private(set) var historyState: LoadState = .idle

    @ObservationIgnored private let client: any ObjectCardFetching
    @ObservationIgnored private var generation = GenerationGuard()

    init(reference: ObjectReference, client: any ObjectCardFetching = ObjectCardClient(config: MemoryConfig())) {
        self.reference = reference
        self.client = client
    }

    /// Fetches the card. A retry supersedes an in-flight load and resets the
    /// history, which is refetched when its section appears again.
    func load() async {
        let generation = generation.next()
        loadState = .loading
        history = []
        historyState = .idle
        do {
            let card = try await client.card(for: reference)
            guard self.generation.isCurrent(generation) else { return }
            self.card = card
            loadState = .loaded
        } catch {
            guard self.generation.isCurrent(generation) else { return }
            Log.net.error("object card load failed: \(error.localizedDescription)")
            loadState = .failed(error.localizedDescription)
        }
    }

    /// Fetches the version history once per card load.
    func loadHistoryIfNeeded() async {
        guard historyState == .idle || isHistoryFailed else { return }
        let generation = generation.current
        historyState = .loading
        do {
            let versions = try await client.history(for: reference)
            guard self.generation.isCurrent(generation) else { return }
            history = versions
            historyState = .loaded
        } catch {
            guard self.generation.isCurrent(generation) else { return }
            Log.net.error("object history load failed: \(error.localizedDescription)")
            historyState = .failed(error.localizedDescription)
        }
    }

    private var isHistoryFailed: Bool {
        if case .failed = historyState {
            return true
        }
        return false
    }
}
