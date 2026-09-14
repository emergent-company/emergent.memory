import XCTest
@testable import MemoryConnector

final class RestartPolicyTests: XCTestCase {

    func testFirstExitsRestart() {
        var policy = RestartPolicy(maxRestarts: 3, window: 60, restartDelay: 3)
        let t0 = Date(timeIntervalSince1970: 1_000)

        XCTAssertEqual(policy.registerExit(at: t0), .restart(after: 3))
        XCTAssertEqual(policy.registerExit(at: t0 + 5), .restart(after: 3))
        XCTAssertEqual(policy.restartCount, 2)
    }

    func testGiveUpAfterMaxRestartsWithinWindow() {
        var policy = RestartPolicy(maxRestarts: 3, window: 60, restartDelay: 3)
        let t0 = Date(timeIntervalSince1970: 1_000)

        // 1st, 2nd, 3rd exit: restart.
        XCTAssertEqual(policy.registerExit(at: t0), .restart(after: 3))
        XCTAssertEqual(policy.registerExit(at: t0 + 10), .restart(after: 3))
        XCTAssertEqual(policy.registerExit(at: t0 + 20), .restart(after: 3))
        // 4th exit inside the window: give up.
        XCTAssertEqual(policy.registerExit(at: t0 + 30), .giveUp)
        XCTAssertEqual(policy.restartCount, 4)
    }

    func testWindowExpiryResetsCount() {
        var policy = RestartPolicy(maxRestarts: 3, window: 60, restartDelay: 3)
        let t0 = Date(timeIntervalSince1970: 1_000)

        _ = policy.registerExit(at: t0)
        _ = policy.registerExit(at: t0 + 10)
        _ = policy.registerExit(at: t0 + 20)
        XCTAssertEqual(policy.restartCount, 3)

        // A gap longer than the window resets the breaker.
        XCTAssertEqual(policy.registerExit(at: t0 + 200), .restart(after: 3))
        XCTAssertEqual(policy.restartCount, 1)
    }

    func testExactlyAtWindowBoundaryDoesNotReset() {
        var policy = RestartPolicy(maxRestarts: 3, window: 60, restartDelay: 3)
        let t0 = Date(timeIntervalSince1970: 1_000)

        _ = policy.registerExit(at: t0)
        // t0+60 is not *greater* than the window, so it still counts.
        XCTAssertEqual(policy.registerExit(at: t0 + 60), .restart(after: 3))
        XCTAssertEqual(policy.restartCount, 2)
    }

    func testDefaults() {
        let policy = RestartPolicy()
        XCTAssertEqual(policy.maxRestarts, 3)
        XCTAssertEqual(policy.window, 60)
        XCTAssertEqual(policy.restartDelay, 3)
        XCTAssertEqual(policy.restartCount, 0)
    }
}
