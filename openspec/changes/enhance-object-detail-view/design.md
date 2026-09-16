## Context

The object detail view (`gateway/objects.templ` → `ObjectDetailPage`) is read-only: a static two-column properties grid, a passive "No relationships" empty state, and no chat entry point. The gateway's `MemoryBackend` interface exposes only graph reads (`GetGraphObject`, `ListGraphObjects`, `GetObjectEdges`, `GetSimilarObjects`, `ListBranches`) — no writes and no object search.

The memory service already exposes everything needed:
- `POST /api/graph/relationships` (create relationship), `PATCH /api/graph/objects/{id}` (patch object, delta-merge properties/labels), `GET /api/graph/objects/fts` (full-text object search).
- Object↔conversation linking via `kb.chat_conversations.canonical_id` — a built-in "refinement chat" mechanism. `CreateConversation` and `StreamChat` both accept `canonicalId` and get-or-create a conversation by it (`GetByCanonicalID`).

So this change is entirely gateway-side: surface the write/search methods and the canonicalId link, and rework the detail-view UI.

## Goals / Non-Goals

**Goals:**
- Make object properties editable (single-column, label-above-content).
- Add relationship creation from the detail view via a modal.
- Add object-linked chat (1:1) with a configurable editor agent.
- Introduce the minimum new gateway data-layer surface to support the above.

**Non-Goals:**
- Multi-object conversations (link is 1:1 via `canonical_id`; a join table would be a separate memory change).
- Object type editing (the patch endpoint has no `type` field — type is schema-level).
- Graph-view editing; the graph render in the browser is out of scope.
- Any memory-service change. Everything reuses existing endpoints.

## Decisions

### D1 — Object search uses full-text search (FTS)

"Find the object to connect" uses `GET /api/graph/objects/fts` (exact-name/token matching), surfaced as a new `MemoryBackend` method with a `type` filter.

- **Rationale**: the modal intent is "I know which object I want" — precise name matching beats similarity ranking. FTS is cheaper and deterministic.
- **Alternatives considered**: `vector-search` (semantic "something like this" — wrong intent, nondeterministic ranking, harder to test); reusing `ListGraphObjects` (no text search, 100-item cap, poor for large graphs).
- **Fallback**: empty query falls back to the existing type-filtered list.

### D2 — Complex property values edit as JSON text

Scalar values render as plain inputs; arrays/objects render as JSON text and are parsed back on save.

- **Rationale**: `Properties` is `map[string]any` — values can be nested/arrays. A single JSON-text strategy covers all cases without a bespoke nested editor.
- **Alternatives considered**: nested schema-driven editor (large, over-engineered); scalar-only (silently drops/breaks complex values).

### D3 — Editor agent is a project setting

New setting category/key mirroring the existing assistant-agent pattern (`settingsAssistantCategory`/`Key` → `{"agentId": id}`): category `editor`, key `agent_id`, value `{"agentId": id}`. Resolution reuses the `mergeAgentID` fallback logic generalized to "default agent → first enabled → first agent → empty".

- **Rationale**: consistent with how remember/dedup/assistant agents are already configured; reuses the settings-page agent selector UI and the generic-settings REST surface.
- **Alternatives considered**: hardcode a fixed agent (inflexible); per-object agent (over-scoped for now).

### D4 — Version-aware redirect after edit

`PATCH /api/graph/objects/{id}` returns a **new** object ID (versioning). After a successful edit, the handler redirects `/objects/:id` → `/objects/<new-id>`.

- **Rationale**: keeps the canonical-id/version-id distinction honest; `GetByAnyID` resolves either form so back/forward and stale links still work.
- **Alternatives considered**: key the detail URL on `canonicalId` instead (cleaner but a larger routing change touching existing links).
- **PRG feedback**: successful saves redirect with `?updated=1` and the detail page flashes "Object updated."; failed saves redirect with `?err=<reason>` so the real error is surfaced instead of silently discarded. Toast delivery is global, not object-specific: the flash script pushes straight into the Alpine toast queue when the shell is loaded (hx-boost in-page swaps) and falls back to the sessionStorage stash on full page loads; partials also carry a `<title>` so htmx keeps `document.title` in sync during boosted navigation (partial responses otherwise have no head).


### D5 — Relationship modal flow

Modal presents: relationship type (from `GetCompiledTypes`, constrained to types whose `SourceType`/`TargetType` match the current object's type), source object (pre-selected to the current object, searchable), target object (searchable). On confirm, `POST /api/graph/relationships` with `{type, src_id, dst_id}`. The current object defaults to source but either endpoint is changeable.

- **Rationale**: schema-constrained type list prevents invalid edges; pre-selecting the current object matches the "connect this thing" mental model.
- **Alternatives considered**: freeform type text (rejects silently, no guidance); current-object-always-source (removes the user's freedom to flip direction).

### D6 — Chat links via `canonicalId`, get-or-create

Add `CanonicalID` to the gateway `ChatRequest` and surface it on conversation types. The "Chat about object" action resolves the editor agent, builds a seeded prompt (mirroring `mergeInstruction`), and deep-links into chat carrying `canonicalId`. Memory's existing get-or-create dedup (`GetByCanonicalID`) provides "resume the object's conversation" for free.

- **Rationale**: reuses memory's built-in refinement-chat link; no gateway-side object→conversation mapping to maintain.
- **Alternatives considered**: gateway-stored mapping + resume-by-`conversationId` (duplicates state memory already holds); pre-create via `POST /api/chat/conversations` (requires a message body, risking a stray placeholder message).

## Risks / Trade-offs

- **[Resume drops the seeded message]** → Memory's `CreateConversation` returns the existing conversation and discards `req.Message` when a `canonicalId` match is found (service.go:87-90). On resume, re-sending the seeded prompt would not persist. **Mitigation**: task breakdown includes verifying the resume path; if the message is dropped, deep-link with `conversationId` (from a gateway get-or-create) instead of `canonicalId`, so the normal resume branch (`AddMessage`) persists the prompt. Confirm exact semantics against the stream handler during implementation.
- **[Patch delta-merge semantics]** → `PATCH` merges properties (not replace) and merges labels unless `ReplaceLabels=true`. A partial edit must send only changed keys, never the whole map, or hidden/removed keys are unaffected (good) but stale keys survive delete (bad). **Mitigation**: explicit add/remove of keys in the edit payload; unit-test the merge behavior.
- **[Non-scalar edit parse failures]** → Invalid JSON typed by the user must not corrupt the object. **Mitigation**: validate/parse client-side and server-side; reject with a clear error and keep the field unedited.
- **[FTS availability]** → If FTS under-performs for a project's schema, the type-filtered list fallback still allows selection. **Mitigation**: keep the fallback path, not a hard dependency.

## Migration Plan

None. Additive gateway changes only: new data-layer methods, a new project setting (created on first save), and a struct field (`Labels`) that begins receiving already-returned data. No database or memory-service migration. Rollback is a normal revert.

## Open Questions

None that would change the specs, approach, or task breakdown — the only remaining unknown (resume-message semantics in D6) is a verification step already scoped into the tasks.
