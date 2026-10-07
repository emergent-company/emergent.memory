@testable import MemoryConnector
import Testing

/// The scope-swap suppression gate: while a scope swap is in flight, the
/// connected-project-id sink must not reconcile. A depth counter (not a
/// boolean) keeps overlapping swaps suppressed until all of them settle.
struct ScopeSwapGateTests {
    @Test func allowedBeforeAnySwap() {
        let gate = ScopeSwapGate()
        #expect(gate.mayReconcile)
    }

    @Test func suppressedBetweenBeginAndEnd() {
        var gate = ScopeSwapGate()
        gate.begin()
        #expect(!gate.mayReconcile)
        gate.end()
        #expect(gate.mayReconcile)
    }

    @Test func allowedAgainAfterLastEnd() {
        var gate = ScopeSwapGate()
        gate.begin()
        gate.end()
        #expect(gate.mayReconcile)
    }

    @Test func twoOverlappingSwapsStaySuppressedUntilBothEnd() {
        var gate = ScopeSwapGate()
        gate.begin()
        gate.begin()
        #expect(!gate.mayReconcile)

        gate.end() // first swap settles
        #expect(!gate.mayReconcile, "one swap still in flight")

        gate.end() // second swap settles
        #expect(gate.mayReconcile)
    }

    @Test func beginEndAreBalanced() {
        var gate = ScopeSwapGate()
        gate.begin()
        gate.begin()
        gate.begin()
        #expect(gate.depth == 3)
        #expect(!gate.mayReconcile)

        gate.end()
        gate.end()
        gate.end()
        #expect(gate.depth == 0)
        #expect(gate.mayReconcile)
    }

    @Test func endWithoutBeginDoesNotGoNegative() {
        var gate = ScopeSwapGate()
        gate.end()
        #expect(gate.depth == 0)
        #expect(gate.mayReconcile)
    }
}
