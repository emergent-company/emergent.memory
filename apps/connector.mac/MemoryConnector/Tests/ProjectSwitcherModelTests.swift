@testable import MemoryConnector
import XCTest

/// The project panel's row model: labelling, markers and section order. Pure
/// values on purpose — this is what keeps the ordering rules testable without a
/// window, a popover or a live store.
final class ProjectSwitcherModelTests: XCTestCase {
    private func project(_ id: String, _ name: String?, org: String? = nil) -> ProjectInfo {
        ProjectInfo(id: id, name: name, orgID: org)
    }

    private func group(_ id: String, _ organization: String?, _ projects: [ProjectInfo])
        -> ProjectStore.ProjectGroup
    {
        ProjectStore.ProjectGroup(id: id, organization: organization, projects: projects)
    }

    // MARK: - Rows

    func testRowCarriesNameAndOrganization() {
        let row = ProjectSwitcherModel.row(project("p1", "Alpha", org: "o1"),
                                           organizationName: "Acme",
                                           connectedIDs: [],
                                           activeID: nil)
        XCTAssertEqual(row.title, "Alpha")
        XCTAssertEqual(row.subtitle, "Acme")
        XCTAssertFalse(row.isActive)
        XCTAssertFalse(row.isConnected)
    }

    func testRowFallsBackToUntitledProject() {
        let row = ProjectSwitcherModel.row(project("p1", "   "),
                                           organizationName: nil,
                                           connectedIDs: [],
                                           activeID: nil)
        XCTAssertEqual(row.title, String(localized: "Untitled project"))
    }

    func testRowOmitsBlankOrganizationSubtitle() {
        let row = ProjectSwitcherModel.row(project("p1", "Alpha"),
                                           organizationName: "  ",
                                           connectedIDs: [],
                                           activeID: nil)
        XCTAssertNil(row.subtitle)
    }

    func testRowMarksActiveAndConnected() {
        let row = ProjectSwitcherModel.row(project("p1", "Alpha"),
                                           organizationName: nil,
                                           connectedIDs: ["p1", "p2"],
                                           activeID: "p1")
        XCTAssertTrue(row.isActive)
        XCTAssertTrue(row.isConnected)
    }

    // MARK: - Sections

    func testNoRecentSectionBelowThreshold() {
        let projects = (1 ... 3).map { project("p\($0)", "Project \($0)", org: "o1") }
        let sections = ProjectSwitcherModel.sections(
            groups: [group("o1", "Acme", projects)],
            recents: projects,
            organizationNames: ["o1": "Acme"],
            connectedIDs: [],
            activeID: nil
        )
        XCTAssertEqual(sections.map(\.title), ["Acme"])
        XCTAssertEqual(sections.first?.rows.count, 3)
    }

    func testNoRecentSectionWhenRecentsAreEmpty() {
        let projects = (1 ... 12).map { project("p\($0)", "Project \($0)", org: "o1") }
        let sections = ProjectSwitcherModel.sections(
            groups: [group("o1", "Acme", projects)],
            recents: [],
            organizationNames: ["o1": "Acme"],
            connectedIDs: [],
            activeID: nil
        )
        XCTAssertEqual(sections.map(\.title), ["Acme"])
    }

    func testRecentSectionLeadsOnceTheListIsLongEnough() {
        let projects = (1 ... 12).map { project("p\($0)", "Project \($0)", org: "o1") }
        let recents = [project("p3", "Project 3", org: "o1"), project("p1", "Project 1", org: "o1")]
        let sections = ProjectSwitcherModel.sections(
            groups: [group("o1", "Acme", projects)],
            recents: recents,
            organizationNames: ["o1": "Acme"],
            connectedIDs: ["p1"],
            activeID: "p3"
        )
        XCTAssertEqual(sections.count, 2)
        XCTAssertEqual(sections[0].title, String(localized: "Recent"))
        // Recent keeps the store's newest-first order.
        XCTAssertEqual(sections[0].rows.map(\.id), ["p3", "p1"])
        XCTAssertEqual(sections[0].rows[0].subtitle, "Acme")
        XCTAssertTrue(sections[0].rows[0].isActive)
        XCTAssertTrue(sections[0].rows[1].isConnected)
        XCTAssertEqual(sections[1].rows.count, 12)
    }

    func testGroupsKeepTheirOrderAndFallBackForUnknownOrganization() {
        let unknown = group("__unknown__", nil, [project("p9", "Loose")])
        let sections = ProjectSwitcherModel.sections(
            groups: [group("o1", "Acme", [project("p1", "Alpha")]), unknown],
            recents: [],
            organizationNames: [:],
            connectedIDs: [],
            activeID: nil
        )
        XCTAssertEqual(sections.map(\.id), ["org:o1", "org:__unknown__"])
        XCTAssertEqual(sections.map(\.title), ["Acme", "Other"])
        XCTAssertNil(sections[1].rows.first?.subtitle)
    }
}
