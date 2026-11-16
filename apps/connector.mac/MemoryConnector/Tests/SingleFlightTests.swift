@testable import MemoryConnector
import XCTest

/// The single-flight guard behind the launch/wake settle: one run at a time, and
/// a guard that is cleared when the run ends.
///
/// The clearing half is the behaviour that matters: a guard that stayed set
/// after the first launch would stop the main window's `.task` from refreshing
/// anything on reopen. Covered here rather than through `AppEnvironment`, whose
/// initializer builds the real stores.
@MainActor
final class SingleFlightTests: XCTestCase {
    func testConcurrentCallersShareOneRun() async {
        let flight = SingleFlight<Int>()
        let runs = CallCounter()
        let work: @MainActor () async -> Int = {
            runs.increment()
            try? await Task.sleep(for: .milliseconds(50))
            return 7
        }

        async let first = flight.run(work)
        async let second = flight.run(work)
        let values = await (first, second)

        XCTAssertEqual(values.0, 7)
        XCTAssertEqual(values.1, 7)
        XCTAssertEqual(runs.count, 1, "the second caller joins the run already in flight")
    }

    func testGuardClearsSoTheNextCallRunsAgain() async {
        let flight = SingleFlight<Int>()
        let runs = CallCounter()
        let work: @MainActor () async -> Int = {
            runs.increment()
            return runs.count
        }

        let first = await flight.run(work)
        XCTAssertFalse(flight.isRunning, "the guard is cleared when the run ends")

        let second = await flight.run(work)

        XCTAssertEqual(first, 1)
        XCTAssertEqual(second, 2)
        XCTAssertEqual(runs.count, 2,
                       "reopening the window must settle again, not join a finished launch")
    }

    func testJoinReturnsImmediatelyWhenNothingIsRunning() async {
        let flight = SingleFlight<Int>()

        await flight.join()

        XCTAssertFalse(flight.isRunning)
    }

    /// A waiter that resumes on the finished run must never see it as still in
    /// flight: the slot is cleared by the run itself, before the task completes.
    /// Clearing it from the owning caller instead left a window in which a wake
    /// that arrived through `join()` was dropped as "already running".
    func testAFinishedRunIsNotInFlightForAWaiter() async {
        let flight = SingleFlight<Int>()
        let began = expectation(description: "the run started")
        let work: @MainActor () async -> Int = {
            began.fulfill()
            try? await Task.sleep(for: .milliseconds(20))
            return 1
        }

        let owner = Task { await flight.run(work) }
        await fulfillment(of: [began], timeout: 2)
        XCTAssertTrue(flight.isRunning, "the run must be joinable while it works")

        await flight.join()
        XCTAssertFalse(flight.isRunning, "a finished run is not in flight, whoever resumes first")

        _ = await owner.value
    }
}
