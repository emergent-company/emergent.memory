import Foundation
@testable import MemoryConnector
import Testing

struct RestartPolicyTests {
    @Test func firstExitsRestart() {
        var policy = RestartPolicy(maxRestarts: 3, window: 60, restartDelay: 3)
        let startTime = Date(timeIntervalSince1970: 1000)

        #expect(policy.registerExit(at: startTime) == .restart(after: 3))
        #expect(policy.registerExit(at: startTime + 5) == .restart(after: 3))
        #expect(policy.restartCount == 2)
    }

    @Test func giveUpAfterMaxRestartsWithinWindow() {
        var policy = RestartPolicy(maxRestarts: 3, window: 60, restartDelay: 3)
        let startTime = Date(timeIntervalSince1970: 1000)

        // 1st, 2nd, 3rd exit: restart.
        #expect(policy.registerExit(at: startTime) == .restart(after: 3))
        #expect(policy.registerExit(at: startTime + 10) == .restart(after: 3))
        #expect(policy.registerExit(at: startTime + 20) == .restart(after: 3))
        // 4th exit inside the window: give up.
        #expect(policy.registerExit(at: startTime + 30) == .giveUp)
        #expect(policy.restartCount == 4)
    }

    @Test func windowExpiryResetsCount() {
        var policy = RestartPolicy(maxRestarts: 3, window: 60, restartDelay: 3)
        let startTime = Date(timeIntervalSince1970: 1000)

        _ = policy.registerExit(at: startTime)
        _ = policy.registerExit(at: startTime + 10)
        _ = policy.registerExit(at: startTime + 20)
        #expect(policy.restartCount == 3)

        // A gap longer than the window resets the breaker.
        #expect(policy.registerExit(at: startTime + 200) == .restart(after: 3))
        #expect(policy.restartCount == 1)
    }

    @Test func exactlyAtWindowBoundaryDoesNotReset() {
        var policy = RestartPolicy(maxRestarts: 3, window: 60, restartDelay: 3)
        let startTime = Date(timeIntervalSince1970: 1000)

        _ = policy.registerExit(at: startTime)
        // startTime+60 is not *greater* than the window, so it still counts.
        #expect(policy.registerExit(at: startTime + 60) == .restart(after: 3))
        #expect(policy.restartCount == 2)
    }

    @Test func defaults() {
        let policy = RestartPolicy()
        #expect(policy.maxRestarts == 3)
        #expect(policy.window == 60)
        #expect(policy.restartDelay == 3)
        #expect(policy.restartCount == 0)
    }
}
