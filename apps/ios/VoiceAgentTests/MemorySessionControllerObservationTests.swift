import Combine
import Foundation
@testable import Memory
import Testing

// MARK: - Fix A regression — nested-store observation forwarding

/// `MemorySessionController` exposes `chatActivity` as a plain `let` property.
/// SwiftUI's `ObservableObject` observation is shallow, so views that observe
/// the controller but read `controller.chatActivity.turns`/`.surfaces` would
/// not re-render when the nested store mutates. The controller must forward
/// `chatActivity.objectWillChange` into its own `objectWillChange`.
struct MemorySessionControllerObservationTests {
    @MainActor
    @Test func mutatingChatActivityTurnsFiresControllerObjectWillChange() throws {
        let suiteName = "MemorySessionControllerObservationTests.turns.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suiteName))
        defer { defaults.removePersistentDomain(forName: suiteName) }

        let controller = MemorySessionController(config: MemoryConfig(defaults: defaults))

        var fired = false
        let cancellable = controller.objectWillChange.sink { _ in
            fired = true
        }
        defer { cancellable.cancel() }

        // Mutate nested `@Published` state on the store (opens a turn).
        controller.chatActivity.userTurnStarted(anchorMessageID: "m1")

        #expect(fired)
    }

    @MainActor
    @Test func mutatingChatActivitySurfacesFiresControllerObjectWillChange() throws {
        let suiteName = "MemorySessionControllerObservationTests.surfaces.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suiteName))
        defer { defaults.removePersistentDomain(forName: suiteName) }

        let controller = MemorySessionController(config: MemoryConfig(defaults: defaults))

        var fired = false
        let cancellable = controller.objectWillChange.sink { _ in
            fired = true
        }
        defer { cancellable.cancel() }

        // Mutate the store-level `@Published surfaces` list via an A2UI event.
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
        controller.chatActivity.apply(.ui(event))

        #expect(fired)
    }
}
