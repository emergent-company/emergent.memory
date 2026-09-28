import Foundation
@testable import Memory
import SwiftUI
import Testing

// MARK: - 6.1 / 6.2 — A2UI surface decode, store merge, render

struct ChatUISurfaceTests {
    @Test func decodesSurfaceMessages() throws {
        let json = #"""
        {"type":"ui","surfaceId":"s1","messages":[
          {"createSurface":{"surfaceId":"s1","catalogId":"memory-basic"}},
          {"updateComponents":{"surfaceId":"s1","components":[{"id":"root","component":"todo","items":[{"label":"a","done":false}]}]}}
        ]}
        """#
        guard case let .ui(surface)? = decodeChatEvent(json) else {
            Issue.record("expected a ui event")
            return
        }
        #expect(surface.surfaceId == "s1")
        try #require(surface.messages.count == 2)
        #expect(surface.messages[0].createSurface?.catalogId == "memory-basic")
        let component = surface.messages[1].updateComponents?.components.first
        #expect(component?.id == "root")
        #expect(component?.component == "todo")
    }

    @Test func decodesSurfaceWithUnknownComponentForFallback() {
        let json = #"{"type":"ui","surfaceId":"s1","messages":[{"updateComponents":{"surfaceId":"s1","components":[{"id":"x","component":"future-card","foo":"bar"}]}}]}"#
        guard case let .ui(surface)? = decodeChatEvent(json) else {
            Issue.record("expected a ui event")
            return
        }
        #expect(surface.messages.first?.updateComponents?.components.first?.component == "future-card")
        #expect(surface.messages.first?.updateComponents?.components.first?.text("foo") == "bar")
    }

    @Test func ignoresMalformedUISurface() {
        #expect(decodeChatEvent(#"{"type":"ui"}"#) == nil)
        #expect(decodeChatEvent(#"{"type":"ui","messages":[]}"#) == nil)
    }

    @MainActor
    @Test func mergesUpdatesAndDeletesSurface() throws {
        let store = ChatActivityStore()
        func apply(_ json: String) {
            guard let event = decodeChatEvent(json) else {
                Issue.record("failed to decode \(json)")
                return
            }
            store.apply(event)
        }

        apply(#"{"type":"ui","surfaceId":"s1","messages":[{"createSurface":{"surfaceId":"s1","catalogId":"memory-basic"}}]}"#)
        apply(#"{"type":"ui","surfaceId":"s1","messages":[{"updateComponents":{"surfaceId":"s1","components":[{"id":"root","component":"code","code":"x"}]}}]}"#)
        try #require(store.surfaces.count == 1)
        #expect(store.surfaces[0].catalogId == "memory-basic")
        #expect(store.surfaces[0].components.count == 1)

        // Same component id updates in place rather than appending.
        apply(#"{"type":"ui","surfaceId":"s1","messages":[{"updateComponents":{"surfaceId":"s1","components":[{"id":"root","component":"code","code":"y"}]}}]}"#)
        try #require(store.surfaces.count == 1)
        try #require(store.surfaces[0].components.count == 1)
        #expect(store.surfaces[0].components[0].text("code") == "y")

        apply(#"{"type":"ui","surfaceId":"s1","messages":[{"deleteSurface":{"surfaceId":"s1"}}]}"#)
        #expect(store.surfaces.isEmpty)
    }

    @MainActor
    @Test func submittedActionMarksCardAnswered() throws {
        let store = ChatActivityStore()
        let json = #"{"type":"ui","surfaceId":"s1","messages":[{"updateComponents":{"surfaceId":"s1","components":[{"id":"root","component":"proposal","kind":"change","summary":"hi"}]}}]}"#
        let event = try #require(decodeChatEvent(json), "failed to decode")
        store.apply(event)
        try #require(store.surfaces.count == 1)
        #expect(store.surfaces[0].submittedActions["root"] == nil)

        store.submitSurfaceAction(surfaceId: "s1", componentId: "root", response: .string("accept"))
        #expect(store.surfaces[0].submittedActions["root"] == .string("accept"))

        store.reset()
        #expect(store.surfaces.isEmpty)
    }

    /// Parity with the server and web renderer: a `ui` event carrying only
    /// `updateComponents` (no `createSurface`) must still open the surface
    /// rather than being dropped.
    @MainActor
    @Test func updateComponentsWithoutCreateSurfaceOpensSurface() throws {
        let store = ChatActivityStore()
        let json = #"{"type":"ui","surfaceId":"s1","messages":[{"updateComponents":{"surfaceId":"s1","components":[{"id":"root","component":"proposal","kind":"change","summary":"hi"}]}}]}"#
        let event = try #require(decodeChatEvent(json), "failed to decode")
        store.apply(event)

        try #require(store.surfaces.count == 1)
        #expect(store.surfaces[0].id == "s1")
        #expect(store.surfaces[0].components.map(\.id) == ["root"])
    }

    @MainActor
    @Test func rendersEveryCatalogCardAndFallback() {
        let ids = ["proposal", "approval", "question", "code", "entity", "object-form", "todo", "result", "mystery"]
        for id in ids {
            let component = ChatUIComponent(id: id, component: id, props: [:])
            _ = A2UIComponentCard(component: component, submittedResponse: nil) { _ in }
        }
    }

    @Test func encodesSurfaceAction() {
        let action = ChatUIAction(componentId: "root", response: .string("accept"))
        guard
            let json = try? encodeChatDecision(.surfaceAction(surfaceId: "s1", action: action)),
            let data = json.data(using: .utf8),
            let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else {
            Issue.record("surface action did not encode")
            return
        }
        #expect(object["type"] as? String == "surfaceAction")
        #expect(object["surfaceId"] as? String == "s1")
        #expect(object["questionId"] == nil)
        let inner = object["action"] as? [String: Any]
        #expect(inner?["componentId"] as? String == "root")
        #expect(inner?["response"] as? String == "accept")
    }
}
