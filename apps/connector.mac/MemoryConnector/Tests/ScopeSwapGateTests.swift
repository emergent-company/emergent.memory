import XCTest
@testable import MemoryConnector

/// The scope-swap suppression gate: while a scope swap is in flight, the
/// connected-project-id sink must not reconcile. A depth counter (not a
/// boolean) keeps overlapping swaps suppressed until all of them settle.
final class ScopeSwapGateTests: XCTestCase {

    func testAllowedBeforeAnySwap() {
        let gate = ScopeSwapGate()
        XCTAssertTrue(gate.mayReconcile)
    }

    func testSuppressedBetweenBeginAndEnd() {
        var gate = ScopeSwapGate()
        gate.begin()
        XCTAssertFalse(gate.mayReconcile)
        gate.end()
        XCTAssertTrue(gate.mayReconcile)
    }

    func testAllowedAgainAfterLastEnd() {
        var gate = ScopeSwapGate()
        gate.begin()
        gate.end()
        XCTAssertTrue(gate.mayReconcile)
    }

    func testTwoOverlappingSwapsStaySuppressedUntilBothEnd() {
        var gate = ScopeSwapGate()
        gate.begin()
        gate.begin()
        XCTAssertFalse(gate.mayReconcile)

        gate.end() // first swap settles
        XCTAssertFalse(gate.mayReconcile, "one swap still in flight")

        gate.end() // second swap settles
        XCTAssertTrue(gate.mayReconcile)
    }

    func testBeginEndAreBalanced() {
        var gate = ScopeSwapGate()
        gate.begin()
        gate.begin()
        gate.begin()
        XCTAssertEqual(gate.depth, 3)
        XCTAssertFalse(gate.mayReconcile)

        gate.end()
        gate.end()
        gate.end()
        XCTAssertEqual(gate.depth, 0)
        XCTAssertTrue(gate.mayReconcile)
    }

    func testEndWithoutBeginDoesNotGoNegative() {
        var gate = ScopeSwapGate()
        gate.end()
        XCTAssertEqual(gate.depth, 0)
        XCTAssertTrue(gate.mayReconcile)
    }
}
