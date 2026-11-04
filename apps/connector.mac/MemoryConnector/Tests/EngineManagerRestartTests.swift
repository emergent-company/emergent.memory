@testable import MemoryConnector
import XCTest

/// The pure restart-sequencing decision: a restart must wait for a live child to
/// exit before spawning its successor (so the successor can bind the loopback
/// management port), and must queue when a transition is already in flight.
final class EngineManagerRestartTests: XCTestCase {
    @MainActor
    func testStartsDirectlyWhenIdle() {
        XCTAssertEqual(EngineManager.restartAction(hasLiveProcess: false, isBusy: false), .startNow)
    }

    @MainActor
    func testTerminatesLiveChildBeforeRestarting() {
        XCTAssertEqual(EngineManager.restartAction(hasLiveProcess: true, isBusy: false), .terminateThenRestart)
        XCTAssertEqual(EngineManager.restartAction(hasLiveProcess: true, isBusy: true), .terminateThenRestart)
    }

    @MainActor
    func testQueuesWhenBusyWithoutALiveChild() {
        XCTAssertEqual(EngineManager.restartAction(hasLiveProcess: false, isBusy: true), .queueRestart)
    }

    @MainActor
    func testGracePeriodIsBounded() {
        XCTAssertGreaterThan(EngineManager.terminateGracePeriod, 0)
        XCTAssertLessThanOrEqual(EngineManager.terminateGracePeriod, 30)
    }
}
