@testable import MemoryConnector
import XCTest

/// The bounded retry around the launch/wake connection settle.
///
/// The policy's job is to retry exactly a transient failure, a fixed number of
/// times, and to stop; the injected `sleep` lets these tests assert the number
/// of attempts and the delays without touching a clock.
@MainActor
final class ReassertRetryPolicyTests: XCTestCase {
    /// Records the delays a policy slept for, in order, and the attempts made.
    @MainActor
    private final class Recorder {
        var delays: [Duration] = []
        var attempts = 0
    }

    private func makePolicy(_ recorder: Recorder, delays: [Duration]) -> ReassertRetryPolicy {
        let sleep: @MainActor (Duration) async -> Void = { delay in
            recorder.delays.append(delay)
        }
        return ReassertRetryPolicy(delays: delays, sleep: sleep)
    }

    /// An attempt that always reports `outcome`, counting its invocations.
    private func attempt(_ recorder: Recorder,
                         always outcome: ProjectStore.ReassertOutcome) -> @MainActor () async -> ProjectStore.ReassertOutcome
    {
        {
            recorder.attempts += 1
            return outcome
        }
    }

    func testSettledFirstAttemptDoesNotSleep() async {
        let recorder = Recorder()
        let policy = makePolicy(recorder, delays: [.seconds(2), .seconds(8)])

        let outcome = await policy.run(attempt(recorder, always: .settled))

        XCTAssertEqual(outcome, .settled)
        XCTAssertEqual(recorder.attempts, 1)
        XCTAssertTrue(recorder.delays.isEmpty)
    }

    func testTransientFailureRetriesWithTheConfiguredDelays() async {
        let recorder = Recorder()
        let policy = makePolicy(recorder, delays: [.seconds(2), .seconds(8)])
        let flaky: @MainActor () async -> ProjectStore.ReassertOutcome = {
            recorder.attempts += 1
            return recorder.attempts < 3 ? .transientFailure : .settled
        }

        let outcome = await policy.run(flaky)

        XCTAssertEqual(outcome, .settled)
        XCTAssertEqual(recorder.attempts, 3, "two delays means up to three attempts")
        XCTAssertEqual(recorder.delays, [.seconds(2), .seconds(8)],
                       "the backoff is the policy's, in order")
    }

    func testTransientFailureStopsAfterTheLastDelay() async {
        let recorder = Recorder()
        let policy = makePolicy(recorder, delays: [.seconds(2), .seconds(8)])

        let outcome = await policy.run(attempt(recorder, always: .transientFailure))

        XCTAssertEqual(outcome, .transientFailure)
        XCTAssertEqual(recorder.attempts, 3, "it gives up instead of retrying forever")
        XCTAssertEqual(recorder.delays, [.seconds(2), .seconds(8)])
    }

    func testNoRetryPolicyAttemptsOnceEvenWhenTransient() async {
        let recorder = Recorder()

        let outcome = await ReassertRetryPolicy.none.run(attempt(recorder, always: .transientFailure))

        XCTAssertEqual(outcome, .transientFailure)
        XCTAssertEqual(recorder.attempts, 1)
    }

    func testNonTransientOutcomesAreNotRetried() async {
        // Only "the server might answer differently later" is worth a retry.
        // Every other outcome is final, and retrying it would hammer the CLI
        // for an answer that cannot change.
        for terminal in [ProjectStore.ReassertOutcome.settled,
                         .noneConnected,
                         .authRequired,
                         .permanentFailure]
        {
            let recorder = Recorder()
            let policy = makePolicy(recorder, delays: [.seconds(2), .seconds(8)])

            let outcome = await policy.run(attempt(recorder, always: terminal))

            XCTAssertEqual(outcome, terminal)
            XCTAssertEqual(recorder.attempts, 1, "\(terminal.rawValue) must not be retried")
            XCTAssertTrue(recorder.delays.isEmpty, "\(terminal.rawValue) must not sleep")
        }
    }
}
