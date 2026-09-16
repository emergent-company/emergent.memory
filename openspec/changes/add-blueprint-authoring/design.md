## Context

See `proposal.md` — Why. Current constraints that shape the approach:

- A blueprint record's manifest is `packs[0].objectTypes[]` / `relationshipTypes[]`
  plus optional `agents[]` (see `gateway/blueprint_manifest.go`), stored as raw
  JSON on the memory side.
- Memory makes a published blueprint **immutable** (publish stamps a sha256
  checksum of the raw manifest; `blueprint_client.go` comments call this out).
  `PUT /api/blueprints/:id` is only meaningful for a draft.
- The gateway already has a working object-type editor and mutation shape built
  for *schema packs*: `ObjectTypeEdit`, `objectTypeMapFromEdit`,
  `applyObjectTypeEdit`, `compiledProperties`, and the Schema-area editor templ.
- Client methods already exist and are unit-tested: `GetBlueprint`,
  `UpdateBlueprint`, `CreateBlueprintVersion`, `PublishBlueprint`,
  `ApplyBlueprint`.
- Mutation routes are gated by the schema write capability via
  `requireSchemaWrite` / `schemaWritePolicy`.

## Goals / Non-Goals

**Goals:**

- Author a project blueprint draft's object types and pack description from the UI.
- Make "release" (publish) an explicit user action; released = immutable.
- Install any chosen version, with release-then-install for still-draft versions.
- Reuse the existing schema editor and manifest merge helpers — no parallel editor.

**Non-Goals:**

- Editing relationship types and blueprint agents in the manifest (deferred;
  relationship-type editing already tracked in
  `docs/tasks/schema-object-type-editing-followups.md`).
- Version diff / "changes since vX" view.
- Editing global/bundled blueprints in place or forking a project draft from a
  global published blueprint (only project-owned drafts are editable in P1).
- Any memory backend change.

## Decisions

**D1 — Edit targets a draft; editing a published version forks a new draft first.**
Published versions are immutable in memory, and silently mutating a definition
that other projects may have installed would be unsafe. The editor only opens for
project-owned drafts; opening it for a published version first calls
`CreateBlueprintVersion` and edits the fork. *Alternative rejected:* allow
in-place edit of published versions — impossible against memory's checksum
immutability and would break provenance.

**D2 — Reuse the schema object-type editor and `ObjectTypeEdit` merge.**
The blueprint manifest's object types have the same shape the schema editor
already edits (`properties`, `description`, `ui`). The editor templ, form parsing,
validation, and `applyObjectTypeEdit` are reused; only the persistence target
changes (manifest pack instead of schema pack). *Alternative rejected:* a raw
JSON/YAML manifest editor — no validation, poor UX, and duplicates the editor.

**D3 — Gateway-side manifest read-modify-write.**
`GetBlueprint` → decode `blueprintManifest` → replace `Packs[0].ObjectTypes`
(merging the edit) → `UpdateBlueprint`. Pack name is immutable; description is
editable. When the base manifest has no pack (agent-only blueprint), a pack is
created from the blueprint name/version so types can be added. *Alternative
rejected:* a new memory endpoint for per-type blueprint edits — unnecessary; the
full-manifest PUT already exists.

**D4 — Release is an explicit, confirmed action.**
"Release" calls `PublishBlueprint` behind a confirmation dialog. It is separate
from install. *Alternative rejected:* auto-publish on install — the user asked
for an explicit release decision before the new version becomes immutable.

**D5 — Install-version requires a released version.**
Per-version install reuses the install-by-id path. A still-draft version's
install action routes through release first (release-then-install). *Alternative
rejected:* auto-release inside install (the existing bundled `ensureManifestApplied`
behavior) — kept explicit here so the release is a deliberate, reviewable step.

**D6 — Capability gate reused.**
All new mutation handlers use `requireSchemaWrite`; rendering the edit/release/
install affordances is non-authoritative (server still rejects without the
capability).

**D7 — Installing an older version is blocked.**
Install-version compares the candidate against the version currently applied for
the same blueprint and rejects an older candidate with a clear error before any
apply call, reusing the existing `versionGreater` comparison semantics. Equal
(re-install) and newer versions are allowed. *Alternative rejected:* warn and
proceed — a downgrade can strand objects against a newer schema, so block rather
than allow a silent regression. (Revisit if a workflow needs deliberate
downgrades.)

## Risks / Trade-offs

- **[Fork-on-open creates draft churn]** → Fork only on an explicit edit/release
  action, not merely on opening the detail page; reuse an existing open draft of
  the same base when one exists.
- **[Global vs project scope confusion]** → The edit action is offered only for
  project-owned drafts; a global published version shows release/install but no
  edit.
- **[Manifest shape variance]** (agent-only, multiple packs) → Normalize to the
  first pack, or synthesize one from the blueprint identity; ignore extra packs
  with a logged warning rather than corrupting them.
- **[Lost updates with concurrent editors]** → Last-write-wins for v1; acceptable
  because drafts are single-project and edits are explicit. Noted, not solved.
- **[Downgrade guard false negatives]** (unparseable/non-semver labels) →
  compare numerically the way `versionGreater` already does, treating missing
  segments as zero; only a strictly older parsed version is blocked.

## Migration Plan

Additive only: new UI routes and handlers; no schema or data migration. Existing
drafts, published versions, and install paths are untouched. Rollback is removing
the routes/UI — drafts remain valid blueprint records.

## Open Questions

None. (A deliberate downgrade path is out of scope; see D7.)
