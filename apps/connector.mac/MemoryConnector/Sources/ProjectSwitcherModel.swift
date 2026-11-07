import Foundation

/// One rendered project row. A plain value so the ordering, labelling and
/// marker rules are unit-testable without instantiating a view.
struct ProjectSwitcherRow: Identifiable, Equatable {
    let id: String
    let title: String
    let isActive: Bool
    let isConnected: Bool
}

/// A headed run of rows. `title` is nil for a single unheaded group.
struct ProjectSwitcherSection: Identifiable, Equatable {
    let id: String
    let title: String?
    let rows: [ProjectSwitcherRow]
}

/// Builds the row list the project panel and the Project page card render.
///
/// The connector mirrors the web console's structure: a "Recent" shortcut
/// section once the list is long enough to need one, then every project
/// grouped under its organisation. The organisation appears once, as the
/// section header — rows carry the project name only, exactly like the web
/// picker, so a grouped list never repeats the same org on every line. The
/// connection state lives on the row (a trailing dot) rather than in a
/// separate section, so a project that is both recent and connected is not
/// listed three times.
enum ProjectSwitcherModel {
    /// The web console only shows its Recent section beyond this many projects;
    /// the connector keeps the same threshold so the two switchers read alike.
    static let recentSectionThreshold = 10

    /// Projects resolved against the loaded list, newest first.
    static func recentRows(_ recents: [ProjectInfo],
                           connectedIDs: Set<String>,
                           activeID: String?) -> [ProjectSwitcherRow]
    {
        recents.map { row($0, connectedIDs: connectedIDs, activeID: activeID) }
    }

    /// The full list, newest-recents first then organisation groups in the
    /// order the store computed.
    static func sections(groups: [ProjectStore.ProjectGroup],
                         recents: [ProjectInfo],
                         connectedIDs: Set<String>,
                         activeID: String?) -> [ProjectSwitcherSection]
    {
        var sections: [ProjectSwitcherSection] = []

        // The groups partition the loaded list, so their row count is the list
        // size without threading a separate count through every caller.
        let totalProjectCount = groups.reduce(0) { $0 + $1.projects.count }
        let recent = recentRows(recents, connectedIDs: connectedIDs, activeID: activeID)
        if totalProjectCount > recentSectionThreshold, !recent.isEmpty {
            sections.append(ProjectSwitcherSection(id: "recent",
                                                   title: String(localized: "Recent"),
                                                   rows: recent))
        }

        for group in groups {
            sections.append(ProjectSwitcherSection(
                id: "org:\(group.id)",
                title: group.sectionTitle,
                rows: group.projects.map {
                    row($0, connectedIDs: connectedIDs, activeID: activeID)
                }
            ))
        }
        return sections
    }

    /// One row. The project name falls back to a localized placeholder. The
    /// organisation is deliberately absent: the enclosing section header
    /// already names it, and repeating it on every row is the two-line layout
    /// the web picker does not have.
    static func row(_ project: ProjectInfo,
                    connectedIDs: Set<String>,
                    activeID: String?) -> ProjectSwitcherRow
    {
        let name = (project.name ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return ProjectSwitcherRow(
            id: project.id,
            title: name.isEmpty ? String(localized: "Untitled project") : name,
            isActive: project.id == activeID,
            isConnected: connectedIDs.contains(project.id)
        )
    }
}
