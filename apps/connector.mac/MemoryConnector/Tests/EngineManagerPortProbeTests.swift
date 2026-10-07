@testable import MemoryConnector
import XCTest

/// The extracted "may we spawn into a held management port?" decision. The
/// socket probe itself is a settable closure; these tests exercise the pure
/// decision and the probe's settability, restoring the probe in `tearDown`.
final class EngineManagerPortProbeTests: XCTestCase {
    private var originalProbe: (() -> Bool)?

    override func setUp() {
        super.setUp()
        originalProbe = ManagementPortProbe.portInUse
    }

    override func tearDown() {
        if let originalProbe {
            ManagementPortProbe.portInUse = originalProbe
        }
        super.tearDown()
    }

    func testAbortsOnlyWhenNoLiveProcessAndPortInUse() {
        XCTAssertTrue(ManagementPortProbe.shouldAbortStartForPortConflict(hasLiveProcess: false, portInUse: true))
        XCTAssertFalse(ManagementPortProbe.shouldAbortStartForPortConflict(hasLiveProcess: false, portInUse: false))
        XCTAssertFalse(ManagementPortProbe.shouldAbortStartForPortConflict(hasLiveProcess: true, portInUse: true))
        XCTAssertFalse(ManagementPortProbe.shouldAbortStartForPortConflict(hasLiveProcess: true, portInUse: false))
    }

    func testNeverAbortWithLiveProcess() {
        // restart()'s just-SIGTERM'd child still holds the port briefly — a
        // live process must never abort the start, port-in-use or not.
        XCTAssertFalse(ManagementPortProbe.shouldAbortStartForPortConflict(hasLiveProcess: true, portInUse: true))
    }

    func testNeverAbortWhenPortFree() {
        XCTAssertFalse(ManagementPortProbe.shouldAbortStartForPortConflict(hasLiveProcess: false, portInUse: false))
    }

    func testManagementPortInUseProbeIsSettable() {
        ManagementPortProbe.portInUse = { true }
        XCTAssertTrue(ManagementPortProbe.portInUse())

        ManagementPortProbe.portInUse = { false }
        XCTAssertFalse(ManagementPortProbe.portInUse())
    }
}
