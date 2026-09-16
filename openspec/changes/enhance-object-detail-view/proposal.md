## Why

The object detail view is read-only and passive: properties render as a static two-column grid, an object with no relationships shows only a large "No relationships" box with no way forward, and there is no way to start a conversation about an object. Users want to actively manage their knowledge graph from this view — edit fields, connect objects, and discuss an object with an agent — rather than just inspect it.

## What Changes

- **Editable properties**: rework the Properties card to a single-column layout (label on one row, content beneath it) and make the object's common fields (key, status, labels) plus its schema-defined properties editable, with saves persisted via the graph object PATCH endpoint.
- **Relationship creation call-to-action**: replace the empty "No relationships" state with a "Connect" action that opens a modal where the user picks a relationship type (schema-constrained) and selects source and target objects via search. The current object is pre-selected as the source by default but is freely changeable.
- **Object-linked chat**: add a "Chat about object" action that opens a refinement conversation linked one-to-one to the object (via the memory service's built-in `canonical_id` mechanism). The agent receives a pre-prepared prompt seeded with the object's data, and the acting editor agent is configurable via a new project setting.
- **New gateway write paths**: create-relationship, update-object (PATCH), and full-text object search, exposed through `MemoryBackend` and `MemoryClient`.
- **Data-shape fixes**: surface `labels` on `GraphObject` and `canonicalId` on chat request/conversation types so the gateway no longer drops them.

## Capabilities

### New Capabilities

- `object-detail-view`: editable object properties, relationship creation from the object detail view, and object-linked refinement chat with a configurable editor agent.

### Modified Capabilities

<!-- none -->

## Impact

- **Data layer** (`gateway/memory.go`, `gateway/backend.go`): add `Labels` to `GraphObject`; add `UpdateObject`, `CreateRelationship`, and full-text object search to `MemoryBackend`/`MemoryClient`; add `CanonicalID` to `ChatRequest` and the conversation types. Memory REST endpoints already exist — no memory-service change required.
- **Handlers/routes** (`gateway/objects.go`, `gateway/main.go`): new/edit handlers for property PATCH, relationship creation, object search, and object-chat deep-link.
- **UI** (`gateway/objects.templ`): single-column editable properties; relationship modal; chat-about-object button.
- **Settings** (`gateway/settings_handlers.go`, settings page): new "editor agent" project setting mirroring the existing assistant-agent setting pattern.
- **Testing**: TDD — unit tests for the handler/query layer (object patch payloads, relationship creation validation, FTS query building, editor-agent resolution, chat deep-link prompt). Deterministic; no timing/environment-dependent tests.
