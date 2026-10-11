@testable import MemoryConnector
import XCTest

/// The footer's project-connection rule: the label + dot are driven by the
/// account sign-in state together with the number of connected projects, and a
/// leftover connected-project id must never read as connected while signed out.
final class ProjectConnectionSummaryTests: XCTestCase {
    func testSignedOutWithStaleConnectedProjectIsNoProjects() {
        let summary = ProjectConnectionSummary(signedIn: false, connectedCount: 1)
        XCTAssertEqual(summary, .noProjects)
        XCTAssertEqual(summary.dot, .orange)
        XCTAssertEqual(String(localized: summary.label), "no projects connected")
    }

    func testSignedInWithOneConnectedProjectIsConnected() {
        let summary = ProjectConnectionSummary(signedIn: true, connectedCount: 1)
        XCTAssertEqual(summary, .connected(count: 1))
        XCTAssertTrue(summary.isConnected)
        XCTAssertEqual(summary.dot, .green)
        XCTAssertEqual(String(localized: summary.label), "1 project connected")
    }

    func testSignedInWithSeveralConnectedProjectsReportsCount() {
        let summary = ProjectConnectionSummary(signedIn: true, connectedCount: 3)
        XCTAssertEqual(summary, .connected(count: 3))
        XCTAssertTrue(summary.isConnected)
        XCTAssertEqual(summary.dot, .green)
        XCTAssertEqual(String(localized: summary.label), "3 projects connected")
    }

    func testSignedInWithoutConnectedProjectIsNoProjects() {
        let summary = ProjectConnectionSummary(signedIn: true, connectedCount: 0)
        XCTAssertEqual(summary, .noProjects)
        XCTAssertFalse(summary.isConnected)
        XCTAssertEqual(summary.dot, .orange)
        XCTAssertEqual(String(localized: summary.label), "no projects connected")
    }

    func testSignedOutWithoutConnectedProjectIsNoProjects() {
        let summary = ProjectConnectionSummary(signedIn: false, connectedCount: 0)
        XCTAssertEqual(summary, .noProjects)
        XCTAssertEqual(summary.dot, .orange)
        XCTAssertEqual(String(localized: summary.label), "no projects connected")
    }
}
