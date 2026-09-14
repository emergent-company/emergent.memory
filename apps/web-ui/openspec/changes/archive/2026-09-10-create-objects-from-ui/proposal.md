## Why

Memory objects are only ever created indirectly — by extraction from chat/documents or by an agent's memory tools. A user who wants to add a person, task, note, or project to their knowledge graph has no way to do so from the web UI; they must either trigger an extraction or ask an agent. Users need a direct, manual path to create a memory object from the UI.

## What Changes

- **Manual object creation**: add a "New object" affordance to the Objects browser (`/objects`) that opens a create form. The form captures the object's type (required, constrained to the project's compiled object types), key, status, labels, and the type's schema-defined properties, and persists it via the memory graph create endpoint.
- **New gateway write path**: add `CreateObject` to `MemoryBackend`/`MemoryClient` (POST `/api/graph/objects`), plus a `CreateObjectRequest` type mirroring memory's `CreateGraphObjectRequest` shape. The memory REST endpoint already exists — no memory-service change required.
- **Schema-driven form**: the type selector and its per-type property inputs are driven by `GetCompiledTypes` (already present in `MemoryClient`), so the form reflects the project's installed schemas rather than hard-coded fields.
- **Redirect to detail**: after a successful create, redirect to the new object's detail view (`/objects/:id`), where existing editing/relationship/chat affordances take over.

## Capabilities

### New Capabilities

- `object-creation`: manual creation of memory objects from the web UI, with a schema-driven form (type, key, status, labels, properties) persisted through the graph create endpoint.

### Modified Capabilities

<!-- none -->

## Impact

- **Data layer** (`gateway/memory.go`, `gateway/backend.go`): add `CreateObject` to `MemoryClient` and the `MemoryBackend` interface; add a `CreateObjectRequest` struct (`type`, `key`, `status`, `properties`, `labels`, `branch_id`). Reuse existing `GetCompiledTypes` for the schema-driven type/property form.
- **Handlers/routes** (`gateway/objects.go`, `gateway/main.go`): new `uiObjectCreate` handler and `POST /objects` route; the objects browser page passes compiled object types to the create form.
- **UI** (`gateway/objects.templ`): a "New object" entry point on the objects browser and a create form/modal rendering the type selector plus schema-defined property inputs; follow existing go-daisy/form patterns.
- **Testing**: TDD — unit tests for the `CreateObject` client method (request marshalling, error propagation) and the `uiObjectCreate` handler (validation of required type, property payload building, redirect to the created object). Deterministic; no timing/environment-dependent tests.
