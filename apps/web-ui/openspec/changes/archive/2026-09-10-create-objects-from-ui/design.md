## Context

The web app already reads and edits memory objects (Objects browser at `/objects`, detail view at `/objects/:id`), but creation is indirect only — via extraction or agent memory tools. `MemoryClient` already has `UpdateObject` (PATCH `/api/graph/objects/:id`), `CreateRelationship` (POST `/api/graph/relationships`), and `GetCompiledTypes` (GET `/api/schemas/projects/:pid/compiled-types`), but no create-object method. Emergent Memory already exposes `POST /api/graph/objects` (`CreateGraphObjectRequest`), returning the created `GraphObject` with a `201`. No memory-service change is needed; this is a gateway-only change.

See proposal.md for motivation and specs/object-creation/spec.md for requirements.

## Goals / Non-Goals

**Goals:**
- Add a direct, schema-driven manual create-object flow in the web UI.
- Reuse the existing compiled-types schema and go-daisy/form patterns; no new dependencies.

**Non-Goals:**
- No branch selection on create — objects are created on the main branch (consistent with the browser's default).
- No bulk/import creation, no relationship creation as part of the create form (existing detail-view relationship flow covers edges).
- No change to extraction or agent-driven object creation.

## Decisions

### 1. Create via direct REST endpoint (`POST /api/graph/objects`)
- **Choice**: add `CreateObject` to `MemoryClient`/`MemoryBackend` with a `CreateObjectRequest` struct mirroring memory's `CreateGraphObjectRequest` (`type`, `key`, `status`, `properties`, `labels`, `branch_id`).
- **Alternatives**: drive creation through an agent chat prompt (non-deterministic, adds LLM latency and failure modes); call the MCP tool (heavier session handshake for a single deterministic write).
- **Rationale**: the REST endpoint is already present and deterministic — ideal for a synchronous, testable UI write.

### 2. Dedicated form page (`GET /objects/new` + `POST /objects`)
- **Choice**: a dedicated create page rendered by `uiObjectNew`, posting to `uiObjectCreate`; on success redirect to `/objects/:id`.
- **Alternatives**: a `<dialog>` modal on the browser page (matches the relationship-create modal, but dynamic per-type property fields are awkward in a compact modal); inline form on the browser page (clutters the list/detail surface).
- **Rationale**: a full page gives room for schema-driven fields, is deep-linkable, and is simpler to test than a modal's JS state.

### 3. Schema-driven fields from `GetCompiledTypes`
- **Choice**: the type selector lists `CompiledSchemaTypes.ObjectTypes`; the selected type's `Properties` (raw JSON, a map of `{name: {type, description, default}}`) drives which property inputs render and what widget each uses.
- **Input-widget mapping** (the memory graph supports exactly these property types — see `domain/graph/validation.go`):
  - `date` → `<input type="date">` (memory coerces ISO 8601 / common date formats)
  - `number` → `<input type="number" step="any">` (handler JSON-parses the string to a number)
  - `boolean` → daisyUI `toggle` switch (hidden `false` input + `class="toggle"` checkbox `true`, so unchecked submits an explicit `false`)
  - `array` / `object` → `form.TagList` chip input (one value per chip, submits `name[idx]`)
  - `string` / any unrecognised type → plain `<input type="text">`
  - Note: `enum`/`minimum`/`maximum` are NOT surfaced by the compiled-types endpoint (its `PropertyDef` carries only `type`/`description`/`default`), so no enum dropdown is rendered.
- **Labels** → `form.TagList` chip input with a combobox autocomplete dropdown. Existing labels are fetched via `ListGraphObjects` (best-effort, `captureError` on failure) and passed as `Suggestions`; the TagList renders a filtered listbox (arrow-key highlight, Enter to select, click to select, Escape to close) excluding already-added tags.
- **Alternatives**: hard-code the known types (drifts from installed schemas); parse the full blueprint schema (heavier, and the compiled endpoint already drops `enum`/`format`).
- **Rationale**: `GetCompiledTypes` is already the source of truth for the browser's type filter and the detail view's relationship types; native HTML inputs give type-correct widgets with no new dependencies.

### 4b. go-daisy `TagList` Alpine fix (dependency change)

`form.TagList` is Alpine-managed. The go-daisy v0.9.0 release had two bugs that made it a no-op:
- `x-init="var root=this; root.addTag=…"` does not bind methods into Alpine 3.15's directive scope, so `@click="addTag()"`/`@keydown="handleKeydown($event)"` resolved to `undefined`.
- the hidden input used `x-bind:name="labels[idx]"` (a static string) instead of a JS template literal, so submitted names were never `labels[0]`, `labels[1]`, …

Fixed upstream in go-daisy (pushed to `master`, commit `dfb83d4`): state + methods are emitted together as an inline `x-data` object literal, and the hidden input name uses `` `labels[${idx}]` ``.

Follow-up (commit `0d85532`): the autocomplete was upgraded from a native `<datalist>` to a Chakra-style combobox dropdown — `filteredSuggestions` computed (case-insensitive, excludes added tags), `open`/`activeIndex` state, ArrowUp/Down highlight, Enter select-or-add, Escape close, `@click.outside` dismiss. alfred pins the fixed commit via a pseudo-version (`v0.9.1-0.…`). Alpine.js core is already loaded by `@alpine.Tag()` in the layout (go-daisy `staticfs`); TagList needs no focus plugin.

### 4. Server-side validation with field preservation
- **Choice**: the handler rejects a missing/invalid `type` and re-renders the form with prior values and an error; memory's own 400 (invalid type/labels) is surfaced as a form error with values preserved.
- **Rationale**: type is required by memory (`validate:"required"`); preserving input on error matches the existing form UX and avoids data loss.

## Risks / Trade-offs

- **Raw `Properties` schema is array-or-map form** → parsing may miss property metadata. *Mitigation*: treat unparseable properties as a generic list of text inputs; key/status/labels remain editable regardless.
- **Memory returns 400 for an invalid type/label** → *Mitigation*: validate type against compiled types client- and server-side before submit; surface memory errors clearly.
- **Type selector empty when no schemas installed** → *Mitigation*: show an empty-state with guidance to install a schema pack rather than a broken form.

## Migration Plan

- Additive change; no data or schema migration. Deploy the gateway build; no rollback beyond reverting the route and template. No config changes.

## Open Questions

- Whether to later add branch selection and label autocomplete to the create form (deferrable; does not change the spec or task breakdown).
