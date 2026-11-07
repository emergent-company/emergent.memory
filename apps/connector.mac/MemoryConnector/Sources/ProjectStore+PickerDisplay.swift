import Foundation

/// Picker-facing read models over `ProjectStore`'s loaded lists.
///
/// These are computed views over state `ProjectStore` already owns (the loaded
/// projects and the connected id set), so they live in an extension file rather
/// than growing the store's body — the store keeps owning lifecycle, not
/// presentation.
extension ProjectStore {
    /// The active project as a full row, when it is in the loaded list. The
    /// picker's trigger needs the organisation id to show the same
    /// org-over-project hierarchy the web console uses.
    var activeProject: ProjectInfo? {
        guard let activeProjectID else { return nil }
        return projects.first { $0.id == activeProjectID }
    }

    /// Projects currently connected (one engine child each), in name order.
    var connectedProjects: [ProjectInfo] {
        projects
            .filter { isConnected($0.id) }
            .sorted { ($0.name ?? "") < ($1.name ?? "") }
    }
}
