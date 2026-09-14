import XCTest
@testable import MemoryConnector

final class PermissionStateTests: XCTestCase {

    func testExitZeroMeansGranted() {
        let state = PermissionCenter.state(fromProbe: 0, stderr: "", timedOut: false, wasRequested: false)
        XCTAssertEqual(state, .granted)
    }

    func testExitZeroWithNoiseStillGranted() {
        let state = PermissionCenter.state(fromProbe: 0, stderr: "some output", timedOut: false, wasRequested: true)
        XCTAssertEqual(state, .granted)
    }

    func test1743ErrorIsDenied() {
        let state = PermissionCenter.state(
            fromProbe: 1, stderr: "execution error: Not authorized to send Apple events to Notes. (-1743)",
            timedOut: false, wasRequested: true)
        XCTAssertEqual(state, .denied)
    }

    func testNotAllowedTextIsDenied() {
        let state = PermissionCenter.state(
            fromProbe: 1, stderr: "osascript is not allowed assistive access",
            timedOut: false, wasRequested: false)
        XCTAssertEqual(state, .denied)
    }

    func testOtherErrorIsUnknownUnlessRequested() {
        let unrequested = PermissionCenter.state(
            fromProbe: 1, stderr: "Notes got an error: can't divide by zero",
            timedOut: false, wasRequested: false)
        XCTAssertEqual(unrequested, .unknown)

        let requested = PermissionCenter.state(
            fromProbe: 1, stderr: "Notes got an error: can't divide by zero",
            timedOut: false, wasRequested: true)
        XCTAssertEqual(requested, .requested)
    }

    func testTimeoutMapsToRequestedWhenFlagged() {
        let flagged = PermissionCenter.state(fromProbe: -1, stderr: "", timedOut: true, wasRequested: true)
        XCTAssertEqual(flagged, .requested)

        let unflagged = PermissionCenter.state(fromProbe: -1, stderr: "", timedOut: true, wasRequested: false)
        XCTAssertEqual(unflagged, .unknown)
    }

    func testGrantedWinsOverNoise() {
        // A granted probe must never be misread as denied from stray output.
        let state = PermissionCenter.state(
            fromProbe: 0, stderr: "not authorized leftovers", timedOut: false, wasRequested: true)
        XCTAssertEqual(state, .granted)
    }

    // MARK: - Persistence

    func testStatePersistenceRoundTrip() {
        let suite = "mc-permission-test-\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite)!
        defer { defaults.removePersistentDomain(forName: suite) }

        XCTAssertEqual(PermissionCenter.loadState(.notes, defaults: defaults), .unknown)
        PermissionCenter.saveState(.granted, for: .notes, defaults: defaults)
        XCTAssertEqual(PermissionCenter.loadState(.notes, defaults: defaults), .granted)

        PermissionCenter.markRequested(.reminders, at: Date(timeIntervalSince1970: 1000), defaults: defaults)
        XCTAssertEqual(PermissionCenter.loadState(.reminders, defaults: defaults), .requested)
        XCTAssertTrue(PermissionCenter.hasRequested(.reminders, defaults: defaults))
        XCTAssertFalse(PermissionCenter.hasRequested(.notes, defaults: defaults))
    }

    func testServiceProbeScripts() {
        XCTAssertTrue(PermissionCenter.Service.notes.probeScript.contains("Notes"))
        XCTAssertTrue(PermissionCenter.Service.reminders.probeScript.contains("Reminders"))
        XCTAssertEqual(PermissionCenter.Service.allCases.count, 2)
    }
}
