@testable import MemoryConnector
import XCTest

/// The "open at login" toggle.
///
/// The system owns the registration, so the model never remembers a flag: it
/// reads the status, applies a change, re-reads, and refuses to show an enabled
/// toggle when macOS did not actually register the app.
@MainActor
final class LaunchAtLoginTests: XCTestCase {
    private enum FakeError: LocalizedError {
        case refused

        var errorDescription: String? {
            "The app is not in a location macOS can register."
        }
    }

    /// Scripted `SMAppService` seam: records what the UI asked for and reports
    /// the state the system would report afterwards.
    private final class FakeService: LaunchAtLoginService, @unchecked Sendable {
        var enabled = false
        var approval = false
        var failure: Error?
        /// The status to report after a successful change, when it differs from
        /// the requested one (`requiresApproval`, or a registration macOS
        /// silently dropped).
        var statusAfterChange: (enabled: Bool, approval: Bool)?
        private(set) var setCalls: [Bool] = []

        var isEnabled: Bool {
            enabled
        }

        var requiresApproval: Bool {
            approval
        }

        func setEnabled(_ enabled: Bool) throws {
            setCalls.append(enabled)
            if let failure {
                throw failure
            }
            if let statusAfterChange {
                self.enabled = statusAfterChange.enabled
                approval = statusAfterChange.approval
            } else {
                self.enabled = enabled
            }
        }
    }

    func testInitialStateReflectsTheSystem() {
        let service = FakeService()
        service.enabled = true
        service.approval = true

        let model = LaunchAtLogin(service: service)

        XCTAssertTrue(model.isEnabled)
        XCTAssertTrue(model.requiresApproval)
        XCTAssertNil(model.lastError)
    }

    func testEnablingRegistersAndReReadsTheStatus() {
        let service = FakeService()
        let model = LaunchAtLogin(service: service)

        model.setEnabled(true)

        XCTAssertEqual(service.setCalls, [true])
        XCTAssertTrue(model.isEnabled)
        XCTAssertNil(model.lastError)
    }

    func testDisablingUnregistersWithoutAnError() {
        let service = FakeService()
        service.enabled = true
        let model = LaunchAtLogin(service: service)

        model.setEnabled(false)

        XCTAssertEqual(service.setCalls, [false])
        XCTAssertFalse(model.isEnabled)
        XCTAssertNil(model.lastError, "turning a setting off successfully is not a failure")
    }

    func testSystemRefusalSurfacesTheErrorAndLeavesTheToggleOff() {
        let service = FakeService()
        service.failure = FakeError.refused
        let model = LaunchAtLogin(service: service)

        model.setEnabled(true)

        XCTAssertFalse(model.isEnabled, "the toggle must not claim a registration that failed")
        XCTAssertEqual(model.lastError, FakeError.refused.localizedDescription)
    }

    func testRegistrationAwaitingApprovalExplainsWhereToApprove() {
        let service = FakeService()
        service.statusAfterChange = (enabled: false, approval: true)
        let model = LaunchAtLogin(service: service)

        model.setEnabled(true)

        XCTAssertFalse(model.isEnabled)
        XCTAssertTrue(model.requiresApproval)
        XCTAssertEqual(model.lastError,
                       "Approve Memory in System Settings › General › Login Items to finish.")
    }

    func testRegistrationDroppedWithoutApprovalIsReported() {
        let service = FakeService()
        service.statusAfterChange = (enabled: false, approval: false)
        let model = LaunchAtLogin(service: service)

        model.setEnabled(true)

        XCTAssertFalse(model.isEnabled)
        XCTAssertEqual(model.lastError, "macOS did not register Memory to open at login.")
    }

    func testRefreshAdoptsAChangeMadeOutsideTheApp() {
        let service = FakeService()
        let model = LaunchAtLogin(service: service)
        XCTAssertFalse(model.isEnabled)

        service.enabled = true
        model.refresh()

        XCTAssertTrue(model.isEnabled, "the system is the source of truth, not a cached flag")
    }
}
