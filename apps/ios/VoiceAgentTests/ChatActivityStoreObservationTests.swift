@testable import Memory
import Observation
import Testing

// MARK: - Observation model — chat store mutations are observable

/// Test-only flag set from `withObservationTracking`'s `@Sendable` `onChange`
/// closure. `@unchecked Sendable` lets the `@Sendable` callback capture the
/// instance; `nonisolated(unsafe)` opts the stored property out of the project's
/// default MainActor isolation so that callback can set it. `onChange` fires
/// synchronously on the mutating (main) thread, so there is no real concurrent
/// access.
private final class ObservationFlag: @unchecked Sendable {
    nonisolated(unsafe) var fired = false
}

/// `ChatActivityStore` is now `@Observable` (no `ObservableObject` /
/// `objectWillChange` forwarding through `MemorySessionController`). Chat
/// surfaces observe it directly from the environment. These tests guard that
/// its mutations remain observable via the Observation framework.
struct ChatActivityStoreObservationTests {
    @MainActor
    @Test func mutatingTurnsIsObservableViaObservationTracking() {
        let store = ChatActivityStore()

        var observedTurnCount = -1
        let onChangeCalled = ObservationFlag()
        withObservationTracking {
            observedTurnCount = store.turns.count
        } onChange: {
            onChangeCalled.fired = true
        }
        #expect(observedTurnCount == 0)

        // Mutate observable state on the store (opens a turn).
        store.userTurnStarted(anchorMessageID: "m1")

        #expect(onChangeCalled.fired)
    }

    @MainActor
    @Test func mutatingSurfacesIsObservableViaObservationTracking() {
        let store = ChatActivityStore()

        let onChangeCalled = ObservationFlag()
        withObservationTracking {
            _ = store.surfaces.count
        } onChange: {
            onChangeCalled.fired = true
        }

        // Mutate the store-level `surfaces` list via an A2UI event.
        let event = ChatUISurfaceEvent(
            surfaceId: "s1",
            messages: [
                ChatUIMessage(
                    createSurface: ChatUICreateSurface(surfaceId: "s1", catalogId: nil),
                    updateComponents: nil,
                    updateDataModel: nil,
                    deleteSurface: nil
                ),
            ]
        )
        store.apply(.ui(event))

        #expect(onChangeCalled.fired)
    }
}
