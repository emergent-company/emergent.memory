## Context

The e2e suite now seeds a bundled schema pack (`personal-memory`, via `POST /api/blueprints/install {source:"bundled", name:"personal-memory"}`) so object/schema/blueprint detail are testable. The remaining gap is **authoring**: object types and blueprint drafts can only come from bundled/registry packs, never from the UI. This change adds UI-first authoring (B.1, B.2) and a deterministic LLM mode (B.3) for the deep tier.

## B.1 — Object-type builder

- **Where**: `/schema` page gets a "New type" action; `/schema/object-types/:name` gets an editor for an existing project type.
- **Data**: a project-scoped object type = name + ordered properties (each: name, type ∈ string/number/boolean/date/array/object, description, required).
- **API**: `POST /api/schema/object-types` (create) and `PUT /api/schema/object-types/:name` (update). The gateway maps this onto the existing compiled-schema path (the same types the object-create form reads from `GetCompiledTypes`).
- **Why not packs**: a bare type is enough to seed a minimal `E2E Note { text }` for tests, avoiding the 20-property `Person`.

## B.2 — Blueprint draft author

- **Where**: the blueprints gallery's "private drafts" concept gains a "New draft" action.
- **Data**: a draft = name + version + description + a set of object types (reusing B.1's editor).
- **Flow**: save draft (private) → install → applies the schema (same `installBlueprint` path). Authoring a draft is the bridge from "I defined types" to "applied schema" without a bundled pack.

## B.3 — Deterministic test LLM (memory backend, cross-repo)

- **Where**: `emergent.memory` (separate repo).
- **Data**: a project flag (e.g. `test_llm: true`) that makes provider generate calls return a canned, deterministic response instead of calling the real provider.
- **Why**: unblocks chat-stream / sandbox / extraction / session-detail / run-detail without live-LLM cost or flakiness.
- **Tracking**: a memory-backend issue/PR, not a gateway spec (no gateway behavior change).

## Verification

- B.1/B.2: gateway `templ generate` + `go build` + `task lint`; e2e specs that create a type/draft via the UI and assert object-create lists it.
- B.3: memory-backend unit test that a flagged project returns the canned response; e2e follow-up gated on the flag being available on dev memory.
