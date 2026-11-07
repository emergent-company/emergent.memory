import Foundation
import Observation

/// Most-recently-used project ids for one signed-in account.
///
/// This is navigation state — what the project switcher offers as a shortcut —
/// not project or engine state, so it lives beside `ProjectStore` rather than
/// inside it. It is persisted in `UserDefaults` under a key namespaced by the
/// account id (the same convention `ProjectStore` uses for the active project),
/// so two accounts never see each other's list.
///
/// Ids are resolved against the loaded project list on read: a project that was
/// deleted or lost access disappears from the list instead of leaving a blank
/// row, without a write on every load.
@MainActor
@Observable
final class RecentProjectsStore {
    /// UserDefaults key prefix; the account id is appended when scoped.
    nonisolated static let keyPrefix = "connector.recentProjectIds"
    /// How many entries the recent list keeps. The web console shows a similar
    /// short list; the switcher only surfaces it once the full list is long
    /// enough to need a shortcut.
    nonisolated static let maxEntries = 5

    private let defaults: UserDefaults
    /// Account the current list belongs to; `nil` before a scope is applied.
    private(set) var accountID: String?
    /// Project ids, newest first.
    private(set) var ids: [String] = []

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    /// Persistence key for the current scope.
    var key: String {
        guard let accountID else { return Self.keyPrefix }
        return "\(Self.keyPrefix).\(accountID)"
    }

    /// Points the store at an account's list (called from the same scope swap
    /// that repoints `ProjectStore`). The stored list is deduplicated and
    /// trimmed on load: a hand-edited or legacy defaults value must never put
    /// the same id in a `ForEach` twice (duplicate SwiftUI identities) or grow
    /// past the cap.
    func applyScope(accountID: String?) {
        self.accountID = accountID
        var seen = Set<String>()
        ids = Array(
            (defaults.stringArray(forKey: key) ?? [])
                .filter { seen.insert($0).inserted }
                .prefix(Self.maxEntries)
        )
    }

    /// Records an explicit project switch as recently used: newest first, one
    /// entry per project, capped at `maxEntries`.
    func record(_ projectID: String) {
        let id = projectID.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !id.isEmpty else { return }
        var next = ids.filter { $0 != id }
        next.insert(id, at: 0)
        if next.count > Self.maxEntries {
            next = Array(next.prefix(Self.maxEntries))
        }
        guard next != ids else { return }
        ids = next
        defaults.set(next, forKey: key)
    }

    /// Recent projects resolved against the loaded list, newest first.
    /// Unresolvable ids are dropped, so the section never renders a blank row.
    func resolved(in projects: [ProjectInfo]) -> [ProjectInfo] {
        let byID = Dictionary(projects.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        return ids.compactMap { byID[$0] }
    }
}
