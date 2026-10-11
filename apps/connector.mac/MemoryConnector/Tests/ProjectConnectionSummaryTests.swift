@testable import MemoryConnector
import XCTest

/// The footer's project-connection rule: the label + dot are driven by the
/// account sign-in state together with the connected project, and a leftover
/// connected-project id must never read as connected while signed out.
final class ProjectConnectionSummaryTests: XCTestCase {
    func testSignedOutWithStaleConnectedProjectIsNoProjects() {
        let summary = ProjectConnectionSummary(signedIn: false, hasConnectedProject: true)
        XCTAssertEqual(summary, .noProjects)
        XCTAssertEqual(summary.dot, .orange)
        XCTAssertEqual(String(localized: summary.label), "no projects connected")
    }

    func testSignedInWithConnectedProjectIsConnected() {
        let summary = ProjectConnectionSummary(signedIn: true, hasConnectedProject: true)
        XCTAssertEqual(summary, .connected)
        XCTAssertEqual(summary.dot, .green)
        XCTAssertEqual(String(localized: summary.label), "1 project connected")
    }

    func testSignedInWithoutConnectedProjectIsNoProjects() {
        let summary = ProjectConnectionSummary(signedIn: true, hasConnectedProject: false)
        XCTAssertEqual(summary, .noProjects)
        XCTAssertEqual(summary.dot, .orange)
        XCTAssertEqual(String(localized: summary.label), "no projects connected")
    }

    func testSignedOutWithoutConnectedProjectIsNoProjects() {
        let summary = ProjectConnectionSummary(signedIn: false, hasConnectedProject: false)
        XCTAssertEqual(summary, .noProjects)
        XCTAssertEqual(summary.dot, .orange)
        XCTAssertEqual(String(localized: summary.label), "no projects connected")
    }
}
