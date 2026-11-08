import Foundation
@testable import Memory
import Testing

/// Holds every injected wait open until the test releases it, so a second tap
/// can land inside a window that has not closed yet.
private actor SleepGate {
    private var arrivals = 0
    private var releaseWaiters: [CheckedContinuation<Void, Never>] = []
    private var arrivalWaiters: [(Int, CheckedContinuation<Void, Never>)] = []

    func sleep(_: Duration) async {
        arrivals += 1
        let ready = arrivalWaiters.filter { $0.0 <= arrivals }
        arrivalWaiters.removeAll { $0.0 <= arrivals }
        for waiter in ready {
            waiter.1.resume()
        }
        await withCheckedContinuation { releaseWaiters.append($0) }
    }

    func waitForArrival(_ count: Int) async {
        while arrivals < count {
            await withCheckedContinuation { arrivalWaiters.append((count, $0)) }
        }
    }

    /// Lets the oldest wait finish; the most recent tap's wait either ends here
    /// or is skipped because its generation is stale.
    func releaseOne() {
        guard !releaseWaiters.isEmpty else { return }
        releaseWaiters.removeFirst().resume()
    }
}

/// The copy confirmation cycle: the checkmark clears after the window, a second
/// tap inside it restarts the window, and the text handed to the pasteboard is
/// exactly what the caller's closure returned.
@MainActor
struct CopyButtonTests {
    @Test func returnsToTheGlyphAfterTheWindow() async {
        let feedback = CopyFeedback(sleep: { _ in })
        var written: [String] = []

        let task = feedback.copy("hello", write: { written.append($0) })
        #expect(feedback.isCopied)
        #expect(written == ["hello"])

        await task.value
        #expect(!feedback.isCopied)
    }

    @Test func aSecondTapInsideTheWindowRestartsIt() async {
        let gate = SleepGate()
        let feedback = CopyFeedback(sleep: { await gate.sleep($0) })
        var written: [String] = []

        let first = feedback.copy("first", write: { written.append($0) })
        await gate.waitForArrival(1)

        let second = feedback.copy("second", write: { written.append($0) })
        await gate.waitForArrival(2)

        // The first window closing must not clear the checkmark the second tap
        // just raised; the second tap's own wait is what ends the window.
        await gate.releaseOne()
        await first.value
        #expect(feedback.isCopied)

        await gate.releaseOne()
        await second.value
        #expect(!feedback.isCopied)
        #expect(written == ["first", "second"])
    }

    @Test func eachTapCopiesTheTextTheClosureReturnsThen() async {
        var current = "one"
        let feedback = CopyFeedback(sleep: { _ in })
        var written: [String] = []

        await feedback.copy(current, write: { written.append($0) }).value
        current = "two"
        await feedback.copy(current, write: { written.append($0) }).value

        #expect(written == ["one", "two"])
    }
}
