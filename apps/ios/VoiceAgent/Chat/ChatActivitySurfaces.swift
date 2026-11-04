import Foundation

// MARK: - A2UI surfaces

extension ChatActivityStore {
    /// Merges one `ui` event's A2UI messages into the surface list:
    /// `createSurface` opens (or updates) a surface, `updateComponents` merges
    /// components by id, `deleteSurface` removes it. `updateDataModel` is
    /// tolerated and ignored (flat component props are rendered directly).
    func applySurface(_ event: ChatUISurfaceEvent) {
        let id = event.surfaceId
        var list = surfaces

        for message in event.messages {
            if message.deleteSurface != nil {
                list.removeAll { $0.id == id }
                continue
            }
            if let create = message.createSurface {
                if let index = list.firstIndex(where: { $0.id == id }) {
                    if let catalogId = create.catalogId {
                        list[index].catalogId = catalogId
                    }
                } else {
                    list.append(ChatUISurface(id: id, catalogId: create.catalogId))
                }
            }
            if let update = message.updateComponents {
                // A `ui` event may carry `updateComponents` without a preceding
                // `createSurface` (the server's `deriveSurfaceID` falls back to a
                // stable default id, and the web renderer draws components
                // directly). Open the surface lazily so the payload is not
                // silently dropped.
                if !list.contains(where: { $0.id == id }) {
                    list.append(ChatUISurface(id: id, catalogId: nil))
                }
                guard let index = list.firstIndex(where: { $0.id == id }) else { continue }
                for component in update.components {
                    if let existing = list[index].components.firstIndex(where: { $0.id == component.id }) {
                        list[index].components[existing] = component
                    } else {
                        list[index].components.append(component)
                    }
                }
            }
        }

        replaceSurfaces(list)
    }

    /// Records a submitted surface action so its card renders answered.
    func submitSurfaceAction(surfaceId: String, componentId: String, response: ChatUIPropValue) {
        guard let index = surfaces.firstIndex(where: { $0.id == surfaceId }) else { return }
        var list = surfaces
        list[index].submittedActions[componentId] = response
        replaceSurfaces(list)
    }
}
