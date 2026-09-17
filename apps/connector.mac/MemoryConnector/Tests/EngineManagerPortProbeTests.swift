import XCTest
@testable import MemoryConnector

/// The extracted "may we spawn into a held management port?" decision. The
/// socket probe itself is a settable closure; these tests exercise the pure
/// decision and the probe's settability, restoring the probe in `tearDown`.
final class EngineManagerPortProbeTests: XCTestCase {

    private var originalProbe: (() -> Bool)?

    override func setUp() {
        super.setUp()
        originalProbe = EngineManager.managementPortInUse
    }

    override func tearDown() {
        if let originalProbe {
            EngineManager.managementPortInUse = originalProbe
        }
        super.tearDown()
    }

    func testAbortsOnlyWhenNoLiveProcessAndPortInUse() {
        XCTAssertTrue(EngineManager.shouldAbortStartForPortConflict(hasLiveProcess: false, portInUse: true))
        XCTAssertFalse(EngineManager.shouldAbortStartForPortConflict(hasLiveProcess: false, portInUse: false))
        XCTAssertFalse(EngineManager.shouldAbortStartForPortConflict(hasLiveProcess: true, portInUse: true))
        XCTAssertFalse(EngineManager.shouldAbortStartForPortConflict(hasLiveProcess: true, portInUse: false))
    }

    func testNeverAbortWithLiveProcess() {
        // restart()'s just-SIGTERM'd child still holds the port briefly — a
        // live process must never abort the start, port-in-use or not.
        XCTAssertFalse(EngineManager.shouldAbortStartForPortConflict(hasLiveProcess: true, portInUse: true))
    }

    func testNeverAbortWhenPortFree() {
        XCTAssertFalse(EngineManager.shouldAbortStartForPortConflict(hasLiveProcess: false, portInUse: false))
    }

    func testManagementPortInUseProbeIsSettable() {
        EngineManager.managementPortInUse = { true }
        XCTAssertTrue(EngineManager.managementPortInUse())

        EngineManager.managementPortInUse = { false }
        XCTAssertFalse(EngineManager.managementPortInUse())
    }
}
