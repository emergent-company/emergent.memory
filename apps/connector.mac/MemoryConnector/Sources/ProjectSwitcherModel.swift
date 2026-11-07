import Foundation

/// One rendered project row. A plain value so the ordering, labelling and
/// marker rules are unit-testable without instantiating a view.
struct ProjectSwitcherRow: Identifiable, Equatable {
    let id: String
    let title: String
    let subtitle: String?
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
/// grouped under its organisation. The connection state lives on the row (a
/// trailing dot) rather than in a separate section, so a project that is both
/// recent and connected is not listed three times.
enum ProjectSwitcherModel {
    /// The web console only shows its Recent section beyond this many projects;
    /// the connector keeps the same threshold so the two switchers read alike.
    static let recentSectionThreshold = 10

    /// Projects resolved against the loaded list, newest first.
    static func recentRows(_ recents: [ProjectInfo],
                           organizationNames: [String: String],
                           connectedIDs: Set<String>,
                           activeID: String?) -> [ProjectSwitcherRow]
    {
        recents.map {
            row($0,
                organizationName: organizationName(for: $0, names: organizationNames),
                connectedIDs: connectedIDs,
                activeID: activeID)
        }
    }

    /// The full list, newest-recents first then organisation groups in the
    /// order the store computed.
    static func sections(groups: [ProjectStore.ProjectGroup],
                         recents: [ProjectInfo],
                         organizationNames: [String: String],
                         connectedIDs: Set<String>,
                         activeID: String?) -> [ProjectSwitcherSection]
    {
        var sections: [ProjectSwitcherSection] = []

        // The groups partition the loaded list, so their row count is the list
        // size without threading a separate count through every caller.
        let totalProjectCount = groups.reduce(0) { $0 + $1.projects.count }
        let recent = recentRows(recents,
                                organizationNames: organizationNames,
                                connectedIDs: connectedIDs,
                                activeID: activeID)
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
                    row($0,
                        organizationName: group.organization,
                        connectedIDs: connectedIDs,
                        activeID: activeID)
                }
            ))
        }
        return sections
    }

    /// One row. Name falls back to a localized placeholder, and the organisation
    /// is the second line (omitted when unknown) — so a single-organisation
    /// account does not repeat the same name on every row.
    static func row(_ project: ProjectInfo,
                    organizationName: String?,
                    connectedIDs: Set<String>,
                    activeID: String?) -> ProjectSwitcherRow
    {
        let name = (project.name ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        let org = (organizationName ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return ProjectSwitcherRow(
            id: project.id,
            title: name.isEmpty ? String(localized: "Untitled project") : name,
            subtitle: org.isEmpty ? nil : org,
            isActive: project.id == activeID,
            isConnected: connectedIDs.contains(project.id)
        )
    }

    static func organizationName(for project: ProjectInfo, names: [String: String]) -> String? {
        guard let orgID = project.orgID, let name = names[orgID], !name.isEmpty else {
            return nil
        }
        return name
    }
}
