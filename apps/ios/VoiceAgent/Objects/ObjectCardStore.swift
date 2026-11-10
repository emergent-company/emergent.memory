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
    /// The last workflow write's failure, shown inline in the strip. Cleared
    /// when the next write starts; the card's old value is never replaced by a
    /// guess, so there is nothing to revert.
    private(set) var actionError: String?
    /// A workflow write is in flight; the pickers are disabled meanwhile.
    private(set) var isSaving = false

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

    // MARK: - Workflow writes

    /// Sets the work item's status through the gateway and adopts the refreshed
    /// card it answers with. On failure the card keeps its old status and
    /// ``actionError`` carries the reason, exactly as the web drawer re-renders
    /// with its action error.
    func setStatus(_ value: String) async {
        await write {
            try await self.client.setStatus(value, for: self.reference)
        }
    }

    /// Sets (or clears, with an empty value) the work item's assignee. Same
    /// failure contract as ``setStatus(_:)``.
    func setAssignee(_ value: String) async {
        await write {
            try await self.client.setAssignee(value, for: self.reference)
        }
    }

    private func write(_ update: () async throws -> ObjectCard) async {
        isSaving = true
        actionError = nil
        defer { isSaving = false }
        do {
            card = try await update()
            loadState = .loaded
        } catch {
            Log.net.error("object card update failed: \(error.localizedDescription)")
            actionError = error.localizedDescription
        }
    }
}
