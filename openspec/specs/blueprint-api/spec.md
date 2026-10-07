# blueprint-api Specification

## Purpose
Expose Emergent Memory's blueprint/schema management as a gateway HTTP API so the
web UI (and future clients) can list, install, toggle, and remove schema packs that
extend the graph database.

## Requirements

### Requirement: List installed blueprint packs
The API SHALL return the blueprint packs currently installed in the project, including
their assignment id, active state, and install timestamp.

#### Scenario: Installed packs returned
- **WHEN** a client sends `GET /api/blueprints/installed`
- **THEN** the API returns HTTP 200 with a JSON array, each element containing `assignmentId`, `schemaId`, `name`, `version`, `description`, `active`, and `installedAt`

#### Scenario: No packs installed
- **WHEN** a client sends `GET /api/blueprints/installed` and no packs are installed
- **THEN** the API returns HTTP 200 with an empty JSON array

#### Scenario: Memory unavailable
- **WHEN** a client sends `GET /api/blueprints/installed` and memory is unreachable
- **THEN** the API returns HTTP 502 with a JSON body containing an `error` field

### Requirement: List available blueprint packs
The API SHALL return packs that are not yet installed, drawn from both the memory schema
registry and the bundled `blueprints/` directory, and SHALL identify each pack's source.

#### Scenario: Available packs returned
- **WHEN** a client sends `GET /api/blueprints/available`
- **THEN** the API returns HTTP 200 with a JSON array, each element containing `id`, `name`, `version`, `description`, `author`, and `source` (either `registry` or `bundled`)

#### Scenario: Bundled packs surfaced
- **WHEN** a pack exists in the local `blueprints/` directory but not in the memory registry
- **THEN** the API includes it in the available list with `source` equal to `bundled`

### Requirement: View compiled schema types
The API SHALL return the project's merged object and relationship types across all active
installed packs.

#### Scenario: Compiled types returned
- **WHEN** a client sends `GET /api/blueprints/compiled-types`
- **THEN** the API returns HTTP 200 with a JSON object containing `objectTypes` and `relationshipTypes` arrays, where each type carries its type name and the pack it originates from

### Requirement: Install a blueprint pack
The API SHALL install a pack into the project by assigning an existing registry schema, or
by registering a bundled pack and then assigning it in a single call.

#### Scenario: Install a registry pack
- **WHEN** a client sends `POST /api/blueprints/install` with a body containing `schemaId` referencing an existing registry schema
- **THEN** the API assigns that schema to the project and returns HTTP 201 with a JSON body containing `assignmentId`, `installedTypes`, `skippedTypes`, and `conflicts`

#### Scenario: Install a bundled pack
- **WHEN** a client sends `POST /api/blueprints/install` with a body containing `source` equal to `bundled` and `name` matching a local pack directory
- **THEN** the API registers the pack in memory and assigns it, returning HTTP 201 with the assignment result

#### Scenario: Install reports conflicts
- **WHEN** an install would overwrite an existing type name
- **THEN** the API returns the assignment result with a non-empty `conflicts` array and does not silently overwrite data

### Requirement: Toggle a pack active
The API SHALL allow enabling or disabling an installed pack without removing its data.

#### Scenario: Disable a pack
- **WHEN** a client sends `PATCH /api/blueprints/assignments/:id` with body `{"active": false}`
- **THEN** the API returns HTTP 200 and the pack no longer contributes its types to the compiled schema

### Requirement: Remove a blueprint pack
The API SHALL soft-remove an installed pack so its schema types stop applying while its data remains recoverable.

#### Scenario: Soft remove
- **WHEN** a client sends `DELETE /api/blueprints/assignments/:id`
- **THEN** the API returns HTTP 204 and the assignment is marked inactive with a removal timestamp

### Requirement: Derive a blueprint draft from current object types

The API SHALL create a blueprint draft whose manifest object and relationship types are assembled from the project's current compiled types, so an edited project schema can be captured as a reusable blueprint.

#### Scenario: Derive a draft

- **WHEN** a client sends `POST /api/blueprints/derive` with a name and optional version and description
- **THEN** the API returns HTTP 201 with a draft blueprint whose object types and relationship types match the project's current compiled types

#### Scenario: Derived draft does not apply

- **WHEN** a blueprint draft is derived from the current object types
- **THEN** the project schema is unchanged until the draft is explicitly installed

#### Scenario: Duplicate name and version

- **WHEN** a client derives a draft whose name and version already exist
- **THEN** the API returns HTTP 409 with an error and creates no blueprint

#### Scenario: Nothing to derive

- **WHEN** a client derives a blueprint but the project has no compiled types
- **THEN** the API returns HTTP 400 with an error and creates no blueprint

### Requirement: Create a new blueprint version from current object types

The API SHALL create a new draft version of an existing blueprint whose manifest types are replaced with the project's current compiled types, leaving prior versions unchanged.

#### Scenario: New version derived

- **WHEN** a client sends a request to derive a new version of an existing blueprint with a new version label
- **THEN** the API returns a new draft version whose object types match the project's current compiled types

#### Scenario: Prior version unchanged

- **WHEN** a new version is derived from an existing blueprint
- **THEN** the existing published version and its manifest remain unchanged

#### Scenario: Unknown base blueprint

- **WHEN** a client derives a new version for a blueprint id that does not exist
- **THEN** the API returns HTTP 404 with an error and creates no blueprint

### Requirement: Blueprint manifests declare board-enabled object types

A blueprint object-type SHALL be able to declare `boardEnabled: true`, an
`allowedStatuses` list of work-status values, and the operational flags
`skipEmbeddings`, `skipExtraction`, and `excludeFromSearch`. These keys SHALL be
carried through the manifest loader and the apply path into the schema registry
unchanged.

#### Scenario: Board-enabled type applied

- **WHEN** a blueprint manifest defines an object type with `boardEnabled:
  true`, `allowedStatuses: [ready, in_progress, review, revision, blocked,
  done]`, and the three operational flags
- **THEN** the applied schema pack preserves those fields on the object type

#### Scenario: Operational flags applied

- **WHEN** a blueprint object type sets `skipEmbeddings`, `skipExtraction`, and
  `excludeFromSearch` to true
- **THEN** the applied type excludes its objects from embeddings, extraction,
  and default search

### Requirement: Blueprint manifests wire agent work and reactions

A blueprint agent definition SHALL be able to declare `workConfig`
(including `requiresReview`, `failureLimit`, and `workContract`), `triggerType`,
`reactionConfig` (object types, events, and concurrency strategy), and a
non-empty `cronSchedule`. These keys SHALL be carried through the manifest
loader and the apply path into the agent definition unchanged.

#### Scenario: Reaction-triggered worker applied

- **WHEN** a blueprint agent declares `triggerType: reaction`,
  `dispatchMode: queued`, and a `reactionConfig` with `objectTypes: [Task]`,
  `events: [created]`, and `concurrencyStrategy: skip`
- **THEN** the applied agent definition wakes on created `Task` objects,
  enqueues rather than running inline, and skips concurrent triggers for the
  same object

#### Scenario: Work config applied

- **WHEN** a blueprint agent declares a `workConfig` with `requiresReview:
  true`, a non-zero `failureLimit`, and `workContract.requireArtifacts: true`
- **THEN** the applied agent definition requires review before done, enforces
  the failure budget, and requires artifacts on completion

#### Scenario: Unstamped runtime agent not adopted

- **WHEN** a runtime agent with the same name as a manifest agent already
  exists but is not stamped with this blueprint's ownership (for example, a
  manually created agent)
- **THEN** applying the blueprint does not adopt or mutate that runtime agent

### Requirement: Blueprint seed objects carry an assignee

A blueprint seed object SHALL be able to declare an `assignee`. On apply, the
seed object's assignee SHALL be stored, routing the object to the matching
listening agent within its project.

#### Scenario: Assigned seed object

- **WHEN** a blueprint seed object of a board-enabled type declares
  `assignee: task-worker`
- **THEN** the applied object carries that assignee and is routed to the
  matching listener when it is `ready`

### Requirement: Gateway bundled-blueprint manifests carry agent work and reaction config

The web-ui gateway SHALL build a bundled blueprint's manifest with the same
agent work/reaction keys the server's `AgentManifest` accepts — `workConfig`,
`triggerType`, `reactionConfig`, `cronSchedule`, `dispatchMode`, `defaultQueue`,
and `maxSteps` — so installing a bundled pack through `POST /api/blueprints`
does not drop a worker agent's object-driven-work wiring.

#### Scenario: Worker agent work and reaction config survives

- **WHEN** a bundled blueprint agent declares `workConfig`, `triggerType`,
  `reactionConfig`, `cronSchedule`, `dispatchMode`, `defaultQueue`, and
  `maxSteps`
- **THEN** `buildBlueprintManifest` emits all those keys in the agent manifest
  JSON, matching the server's `AgentManifest` shape, and `POST /api/blueprints`
  accepts them without dropping any

### Requirement: Gateway bundled-blueprint manifests carry skills and seed

The web-ui gateway SHALL carry a bundled blueprint's `skills/` and `seed/`
contents into its manifest — skills plus seed objects and relationships — so a
bundled pack's workflow skill and keyed seed objects install out of the box
rather than being omitted.

#### Scenario: Workflow skill carried

- **WHEN** a bundled blueprint has a `skills/<name>/SKILL.md` whose frontmatter
  declares `name` and `description`
- **THEN** the gateway manifest lists that skill with its name, description,
  and Markdown body as `content`

#### Scenario: Seed object with assignee carried

- **WHEN** a bundled blueprint has a `seed/objects/<Type>.jsonl` holding a
  keyed, board-enabled object with an `assignee`
- **THEN** the gateway manifest lists that seed object with its `assignee`
  intact, so it routes to the matching listener on apply

### Requirement: Gallery lists the task-board sample

The web-ui gateway SHALL embed the `task-board` sample blueprint, so the
available-blueprints list surfaces it alongside the other bundled packs and
installs a board-enabled `Task` type plus the reaction-triggered
`task-worker` agent.

#### Scenario: task-board surfaced and installs correctly

- **WHEN** the gateway lists embedded blueprints
- **THEN** `task-board` appears with its `Task` object type and `task-worker`
  agent, and installing it produces a board-enabled `Task` type and a
  `task-worker` agent carrying its `workConfig` and `reactionConfig`

### Requirement: GitHub URL blueprint import

The API SHALL import a blueprint from a GitHub repository URL: fetch the
archive, extract and validate its manifest, and create and publish a blueprint
through the existing create/publish service path, scoped to the caller's
project (global only for an authorized superadmin), matching the existing
blueprint-create authorization guards.

The endpoint is `POST /api/blueprints/import` with body
`{url, ref?, token?, modelOverrides?, apply?}`. `modelOverrides` maps an agent
`name` to a `provider/model` reference (or an empty string to select the project
default); each named agent's model SHALL be rewritten in the stored manifest
before the blueprint is created. When `apply` is true, the import SHALL require a
project context, publish the blueprint, and apply it atomically — on an apply
failure the just-created row SHALL be hard-deleted so a retry re-imports cleanly
(no 409), and the apply error is returned.

Only `https://github.com/<org>/<repo>` URLs are accepted, with an optional
`#<ref>` fragment and/or `/tree/<ref>` suffix; every other host and all
non-https schemes are rejected. The archive is fetched only from
`https://codeload.github.com/...` (a fixed origin built from the validated
org/repo/ref segments); the final URL and every redirect hop must be https and
on the exact allowlisted host (a redirect to a non-allowlisted host or to a
non-https scheme is refused). The caller-supplied URL must match the exact host
`github.com` (no port is accepted), so no arbitrary host is ever resolved;
additionally, as defence in depth, IP-literal hosts, localhost, and internal
host suffixes are rejected. Private/link-local/loopback/CGNAT/metadata address
ranges are therefore never reachable — not via an address-range check, but
because only the two exact public GitHub hosts (`github.com` for parsing and
`codeload.github.com` for the fetch) are ever dialed. The archive download is
capped at 50 MiB and the request is bounded by a timeout. Extraction rejects
path traversal (`../`), absolute paths, and symlinks/hardlinks escaping the
archive root, and caps the file count at 20000 and total extracted bytes at
200 MiB. The optional `token` is used only as an outbound Authorization header;
it is never persisted, never logged, and never echoed in responses or errors.
Errors map to 400 for an invalid URL/manifest, 413 for an oversize archive, 502
for a fetch failure, and 401/403 for authorization failures.

#### Scenario: Allowlisted URL accepted

- **WHEN** a client sends `POST /api/blueprints/import` with a body whose `url`
  is `https://github.com/<org>/<repo>` (with an optional `#<ref>` and/or
  `/tree/<ref>`)
- **THEN** the request proceeds to fetch from the `codeload.github.com`
  allowlist and does not return 400 for the URL

#### Scenario: Non-github host rejected

- **WHEN** a client sends `POST /api/blueprints/import` with a `url` whose host
  is not `github.com` (for example, `https://example.com/org/repo`)
- **THEN** the API returns HTTP 400 and performs no fetch

#### Scenario: Non-https scheme rejected

- **WHEN** a client sends `POST /api/blueprints/import` with a `url` using a
  non-https scheme (for example, `http://github.com/org/repo` or
  `git+ssh://github.com/org/repo`)
- **THEN** the API returns HTTP 400 and performs no fetch

#### Scenario: Redirect to non-allowlisted host or non-https refused

- **WHEN** the allowlisted fetch endpoint responds with a redirect whose target
  host is not allowlisted (not `codeload.github.com`), or whose scheme is not
  `https`
- **THEN** the API refuses the redirect, performs no further fetch, and maps the
  failure to HTTP 502

#### Scenario: Oversize archive rejected

- **WHEN** the fetched archive exceeds 50 MiB
- **THEN** the API aborts the download and returns HTTP 413 with no blueprint
  created

#### Scenario: Traversal or symlink archive rejected

- **WHEN** the fetched archive contains a path-traversal entry (`../`), an
  absolute path, or a symlink/hardlink escaping the archive root
- **THEN** extraction is rejected and the API returns HTTP 400 with no
  blueprint created

#### Scenario: Valid manifest creates a published blueprint in the caller's project

- **WHEN** a valid archive with a valid manifest is imported by an authorized
  caller
- **THEN** the API creates and publishes a blueprint scoped to the caller's
  project (global only for an authorized superadmin) and returns success

#### Scenario: Model override rewrites the stored manifest

- **WHEN** a client sends `POST /api/blueprints/import` with a `modelOverrides`
  mapping an agent name to a `provider/model` reference
- **THEN** the stored blueprint manifest carries that reference for the named
  agent, and an empty-string override clears the model name (project default)

#### Scenario: Atomic apply rolls back on failure

- **WHEN** a client sends `POST /api/blueprints/import` with `apply: true` and
  the apply fails
- **THEN** the API returns the apply error, leaves no published blueprint row,
  and a retry with the manifest fixed re-imports without a 409

#### Scenario: Atomic apply requires a project context

- **WHEN** a client sends `POST /api/blueprints/import` with `apply: true` but no
  project context
- **THEN** the API returns HTTP 400 and creates nothing

#### Scenario: Private token not leaked

- **WHEN** a client supplies a `token` and the import fails for any reason
- **THEN** the token is used only as an outbound Authorization header and
  appears in neither the response body, any error message, nor the logs

#### Scenario: Unauthenticated rejected

- **WHEN** an unauthenticated client sends `POST /api/blueprints/import`
- **THEN** the API returns HTTP 401 and performs no fetch and creates no
  blueprint

#### Scenario: Insufficient role rejected

- **WHEN** an authenticated caller lacking the required capability sends
  `POST /api/blueprints/import`
- **THEN** the API returns HTTP 403 and performs no fetch and creates no
  blueprint

### Requirement: Compiled types preserve board work config for array-form object-type schemas

The compiled-types path (`GetCompiledTypesByProject`) SHALL preserve the
object-driven work configuration declared on an object type — `boardEnabled`,
`allowedStatuses`, `skipEmbeddings`, `skipExtraction`, and
`excludeFromSearch` — regardless of whether the pack's `object_type_schemas`
column is stored in the array format (user files and blueprint sample YAML such
as `blueprints/task-board/schemas/task-board.yaml`) or the map format (blueprint
seeds). This SHALL match the fields the runtime extraction normalisation already
preserves. The compiled output for a board-enabled `Task` type declared as an
array entry SHALL carry `boardEnabled: true` and its declared
`allowedStatuses`.

#### Scenario: Array-form board type compiles with its work config

- **WHEN** a schema pack stores object types as a JSON array and one entry
  declares `boardEnabled: true`, `allowedStatuses: [ready, in_progress, review,
  revision, blocked, done]`, and the operational skip flags
- **THEN** the compiled object type for that entry carries `boardEnabled: true`,
  the declared `allowedStatuses`, and the skip flags

#### Scenario: Map-form board type compiles with its work config

- **WHEN** a schema pack stores object types as a JSON map and one entry
  declares a board work config
- **THEN** the compiled object type carries the same field values, so the two
  storage formats are consistent

#### Scenario: Scope-key alias handling is unaffected

- **WHEN** an array entry declares both `scopeKey` and its `scope_key` alias
- **THEN** the canonical `scopeKey` wins and the snake-case alias does not
  survive the array-to-map normalisation

### Requirement: Blueprint relationship types support plural source and target types

A blueprint pack's relationship type SHALL be able to declare `sourceTypes`
and/or `targetTypes` as arrays in addition to the singular `sourceType` and
`targetType`, matching the CLI loader. On apply, each plural declaration SHALL
be expanded into the cross-product of source types × target types and posted to
the schemas service as singular `sourceType`/`targetType` entries, so a
relationship declared with `sourceTypes: [Alpha, Beta]` and `targetType: Gamma`
registers two relationship schemas (Alpha→Gamma and Beta→Gamma).

#### Scenario: Plural source types expand to singular schemas

- **WHEN** a blueprint pack declares a relationship type with `sourceTypes:
  [Alpha, Beta]` and `targetType: Gamma`, and the pack defines the object types
  Alpha, Beta, and Gamma
- **THEN** applying the blueprint registers TWO relationship schemas for that
  relationship name — `Alpha→Gamma` and `Beta→Gamma` — each carrying a non-empty
  singular `sourceType` and `targetType`

#### Scenario: Singular declarations are unchanged

- **WHEN** a blueprint pack declares a relationship type with only the singular
  `sourceType` and `targetType`
- **THEN** applying the blueprint registers exactly one relationship schema,
  unchanged

#### Scenario: Plural declarations round-trip in the stored manifest

- **WHEN** a blueprint imported from a GitHub URL declares plural
  `sourceTypes`/`targetTypes`
- **THEN** the stored blueprint manifest preserves the plural fields, and the
  import does not fail with a missing-`sourceType` schema error

### Requirement: Blueprint relationship-type expansion is bounded per pack

Because plural declarations expand as a cross-product, the total number of
singular relationship-type definitions produced for a single pack SHALL be
bounded. The per-pack limit SHALL be 10 000 expanded definitions. When a pack's
expansion would exceed the limit, apply SHALL fail with `400 bad_request` whose
message names the offending definition, and SHALL reject the pack before
allocating the expanded output, so an over-budget pack cannot cause a
memory-exhausting allocation. A pack expanding to exactly the limit SHALL be
accepted; only totals strictly greater than the limit are rejected. The stored
blueprint manifest is unaffected — the bound applies only to the singular
payload built for the schemas service.

#### Scenario: Over-budget expansion is rejected before allocation

- **WHEN** a blueprint pack's relationship types would expand to more than
  10 000 singular definitions — whether one definition with 10 001 source types
  or several individually-modest definitions whose cumulative total exceeds
  10 000
- **THEN** applying the blueprint fails with `400 bad_request`, the error message
  names the definition whose expansion crossed the limit, and no expanded
  definition list is materialized

#### Scenario: Expansion at the limit is accepted

- **WHEN** a blueprint pack's relationship types expand to exactly 10 000
  singular definitions
- **THEN** applying the blueprint succeeds and registers all 10 000 relationship
  schemas

### Requirement: Blueprint agent visibility uses the server enum

A blueprint agent manifest's `visibility` field SHALL use the server's canonical agent visibility levels — `project`, `external`, or `internal` — matching the `AgentVisibility` constants and `NormalizeVisibility` in `apps/server/domain/agents/entity.go`. An absent or empty value SHALL be treated as `project` (the server default). The offline CLI validator SHALL accept exactly these values and reject any other value (in particular the legacy `public`, `private`, and `system`), and its error message SHALL name the canonical set so a blueprint that passes offline validation also installs successfully.

#### Scenario: Canonical visibility accepted offline

- **WHEN** a blueprint agent manifest declares `visibility: project`, `visibility: external`, `visibility: internal`, or omits `visibility`
- **THEN** `memory blueprints validate` reports no visibility error for that agent

#### Scenario: Legacy visibility rejected offline

- **WHEN** a blueprint agent manifest declares `visibility: public`, `visibility: private`, or `visibility: system`
- **THEN** `memory blueprints validate` reports a visibility error whose message lists `project, external, internal`

### Requirement: Blueprint apply reconciles seed data through batched bulk upsert

The blueprint/seed apply path SHALL resolve every seed object key in one batched
multi-key lookup and apply objects and relationships through the bulk object and
relationship endpoints rather than per-key lookups and per-record upserts, so
re-applying a blueprint or seed pack scales with the batch count rather than the
object count.

#### Scenario: All seed keys resolved in one lookup

- **WHEN** the apply path applies a seed pack containing many keyed objects
- **THEN** it issues a batched multi-key lookup for the seed keys instead of one
  lookup per object

#### Scenario: Objects applied via bulk endpoints

- **WHEN** the apply path applies a seed pack's objects
- **THEN** new objects are inserted via the objects bulk-create endpoint and
  already-existing objects (under `--upgrade`) are updated via the objects
  bulk-upsert endpoint

#### Scenario: Relationships applied via bulk upsert

- **WHEN** the apply path applies a seed pack's relationships
- **THEN** the relationships are idempotently applied via the relationships
  bulk-upsert endpoint

### Requirement: Seed reconciliation preserves per-item errors and counters

The apply path SHALL preserve per-item error reporting and the existing
created/updated/skipped/failed counters when switching to the batched bulk-upsert
path, so a bad seed record is reported without aborting the rest of the apply.

#### Scenario: Per-item failure reported without aborting

- **WHEN** one seed object or relationship fails to apply while the rest succeed
- **THEN** the apply reports that record as failed, continues applying the
  remaining records, and returns a summary with the correct created/updated/
  skipped/failed counts

### Requirement: Seed objects forward assignee and status through the bulk path

Seed objects SHALL continue to carry their `assignee` and `status` values, now
forwarded through the bulk-upsert item shape, so board-enabled seed objects still
route to the matching listener on apply.

#### Scenario: Assignee and status forwarded

- **WHEN** a keyed, board-enabled seed object declares `assignee` and `status`
- **THEN** the bulk-upsert item forwards both fields and the applied object
  carries them unchanged

### Requirement: Validate manifest before materialization on apply

The blueprint apply path SHALL validate a manifest's agents (name, visibility,
and model reference), packs (name and version), and skills (name/description/
content) BEFORE materializing any pack, agent, or skill. A manifest whose agent
model reference is malformed (for example a bare name missing the `provider/`
prefix) SHALL fail with HTTP 400 and zero rows created, rather than
materializing packs first and leaving a half-applied blueprint behind.

#### Scenario: Malformed agent model rejected with no partial apply

- **WHEN** a blueprint is applied whose manifest has an agent model missing the
  `provider/` prefix
- **THEN** the apply fails with HTTP 400 and no pack, agent, or skill is created

#### Scenario: Empty model name selects the provider default

- **WHEN** a blueprint manifest's agent has a model with an empty name
- **THEN** the apply accepts it as the provider default and proceeds

### Requirement: Preview a GitHub blueprint import without creating anything

The API SHALL expose `POST /api/blueprints/import/preview` that performs the same
fetch, extract, validate, and model-override pipeline as the import but creates
NOTHING, returning HTTP 200 with the blueprint's `name`, `version`,
`description`, `author`, and the final `manifest` JSON so a caller can map agent
models before committing the import. The endpoint SHALL enforce the same
scope/authority gating as import (project context → project scope; no project →
global write requires superadmin).

#### Scenario: Preview returns metadata and manifest

- **WHEN** a client sends `POST /api/blueprints/import/preview` with a valid
  GitHub URL
- **THEN** the API returns HTTP 200 with the blueprint's name, version,
  description, author, and manifest, and creates no blueprint

#### Scenario: Preview applies model overrides

- **WHEN** a client sends `POST /api/blueprints/import/preview` with
  `modelOverrides`
- **THEN** the returned manifest carries the overridden agent model references

#### Scenario: Preview rejected for insufficient role

- **WHEN** an authenticated caller without the required capability sends
  `POST /api/blueprints/import/preview`
- **THEN** the API returns HTTP 403 and performs no fetch and creates nothing

### Requirement: CLI blueprint install validates and normalizes models before applying

`memory blueprints install <source>` SHALL run the offline blueprint validation before making any API call that mutates the project, and SHALL abort with a non-zero exit and an actionable error when validation reports any error, leaving the project unmodified. The error SHALL name the offending resource (agent), its model reference, and the expected `provider/model` form.

Before applying, install SHALL normalize every agent `model` reference through the shared `modelref` parser: a structured `model.provider` combined with a bare `model.name` SHALL be folded into the canonical `provider/model` name and the provider field cleared, so the reference sent to the server is always one the server's agent write path accepts. A model that itself contains `/` (for example a Vertex resource path) SHALL be preserved intact. A blueprint that passes offline validation SHALL install as before.

#### Scenario: Invalid bare model aborts with no mutation

- **WHEN** a blueprint agent declares a `model.name` that is not in `provider/model` form (for example `gemini-2.5-flash`)
- **THEN** `memory blueprints install` fails before any API call, reporting the agent, the offending model, and the expected `provider/model` form, and no pack, agent, skill, or seed data is created or modified

#### Scenario: Structured provider normalized before apply

- **WHEN** a blueprint agent declares `model: { name: gpt-4o, provider: openai }`
- **THEN** install sends the canonical reference `openai/gpt-4o` to the server with the provider field cleared

#### Scenario: Valid blueprint installs as before

- **WHEN** a blueprint passes offline validation
- **THEN** install applies its packs, agents, skills, and seed data exactly as before, reporting the same results

### Requirement: GitHub import records source provenance

A blueprint imported from a GitHub repository SHALL persist its source
provenance on the blueprint record: the source kind, a normalized source URL,
and the source ref. The source kind SHALL be `github`; the source URL SHALL be
`https://github.com/<org>/<repo>` with no ref suffix and no `.git`; the source
ref SHALL be the ref the archive was fetched at, with the `HEAD` sentinel
normalized to the empty string to mean the repository default branch.

#### Scenario: Import records kind, URL, and ref

- **WHEN** a caller imports a blueprint from
  `https://github.com/<org>/<repo>` with an explicit ref (via the `ref` field,
  a `#<ref>` fragment, or a `/tree/<ref>` suffix)
- **THEN** the created blueprint carries `sourceKind` `github`, `sourceUrl`
  `https://github.com/<org>/<repo>`, and `sourceRef` equal to that ref

#### Scenario: Default ref normalized to empty

- **WHEN** a caller imports a blueprint from `https://github.com/<org>/<repo>`
  without specifying a ref
- **THEN** the created blueprint carries `sourceRef` equal to the empty string
  (the repository default branch), not `HEAD`

#### Scenario: `.git` suffix stripped from the source URL

- **WHEN** a caller imports a blueprint from
  `https://github.com/<org>/<repo>.git`
- **THEN** the recorded `sourceUrl` is `https://github.com/<org>/<repo>`
  (no `.git`)

#### Scenario: Non-imported blueprints carry no provenance

- **WHEN** a blueprint is created through the ordinary create path (not a GitHub
  import)
- **THEN** its `sourceKind`, `sourceUrl`, and `sourceRef` are empty

### Requirement: Created blueprints carry a canonical manifest checksum

`CreateBlueprint` SHALL compute and persist the canonical manifest checksum at
create time — using the same sha256-over-JSON algorithm `PublishBlueprint` uses
— so a draft carries a checksum for drift comparison without requiring a
publish step.

#### Scenario: Draft carries a non-empty checksum

- **WHEN** a blueprint draft is created
- **THEN** the created blueprint's checksum is a non-empty sha256 hash of its
  JSON manifest

### Requirement: Source provenance survives publish and apply

Publishing a blueprint SHALL NOT clear its source provenance, and applying it
SHALL NOT alter the provenance fields; the provenance recorded at import time
SHALL be preserved for the life of the blueprint record.

#### Scenario: Publish preserves provenance

- **WHEN** a GitHub-imported blueprint is published
- **THEN** its `sourceKind`, `sourceUrl`, and `sourceRef` are unchanged

#### Scenario: Apply preserves provenance

- **WHEN** a GitHub-imported blueprint is applied to a project
- **THEN** its `sourceKind`, `sourceUrl`, and `sourceRef` are unchanged

### Requirement: Provenance-based update detection

A blueprint imported from a GitHub repository SHALL be checked against its
upstream source: the server fetches the upstream manifest at the recorded source
URL/ref, and compares the upstream version and manifest content against the
applied blueprint. An update is reported when the upstream version is strictly
newer, or when the version is unchanged but the manifest content differs.

#### Scenario: Newer upstream version reported

- **WHEN** the upstream repository serves a version newer than the applied
  blueprint (semver-compared where both parse)
- **THEN** the check records `updateAvailable: true` and the newer `latestVersion`

#### Scenario: Same version and content reports no update

- **WHEN** the upstream version and manifest content match the applied blueprint
- **THEN** the check records `updateAvailable: false`

#### Scenario: Same version but changed content reports an update

- **WHEN** the upstream version is unchanged but the manifest content differs
- **THEN** the check records `updateAvailable: true`

#### Scenario: Locally-overridden import is not a false positive

- **WHEN** an applied GitHub blueprint's stored manifest was rewritten by
  project model overrides and the upstream version is unchanged
- **THEN** the check compares against the raw upstream manifest hash recorded at
  import and records `updateAvailable: false`

#### Scenario: A failed check preserves the last known result

- **WHEN** a check fails (network error, deleted source) after a prior successful
  check recorded an update
- **THEN** the previous `updateAvailable` and `latestVersion` are retained and
  only the error and last-checked timestamp are updated

### Requirement: Periodic always-on update check

The server SHALL periodically enqueue one `check` job per applied,
GitHub-imported blueprint, so update status stays current without manual
intervention.

#### Scenario: Sweep enqueues a job per applied GitHub blueprint

- **WHEN** the periodic sweep runs and a project has an applied blueprint whose
  `sourceKind` is `github`
- **THEN** one pending `check` job is enqueued for that (project, blueprint)

#### Scenario: Sweep skips non-GitHub blueprints

- **WHEN** the periodic sweep runs and a project has only non-GitHub applied
  blueprints
- **THEN** no `check` job is enqueued

### Requirement: Durable job queue with retry and backoff

Update-check jobs SHALL be durably stored and claimed with `FOR UPDATE SKIP
LOCKED` so multiple instances can run concurrently without double-claiming.
Failed checks SHALL be retried with exponential backoff (capped), and terminal
failures recorded with an error.

#### Scenario: Job claim is multi-instance safe

- **WHEN** several instances claim pending jobs concurrently
- **THEN** each pending job is claimed by at most one instance

#### Scenario: Failed check retries with backoff

- **WHEN** a check fails before exhausting its attempt budget
- **THEN** the job is returned to pending and is not immediately re-claimable
  (backoff scales with the attempt count)

#### Scenario: Exhausted job fails terminally

- **WHEN** a check keeps failing past the attempt cap
- **THEN** the job is marked failed with a `lastError`

#### Scenario: Orphaned running job is reclaimed

- **WHEN** a job is left `running` past its lease because the process crashed
  or restarted mid-check
- **THEN** it is claimed again by the runner rather than permanently blocking
  that blueprint's future checks

### Requirement: Encrypted project GitHub credential

A project's GitHub personal access token SHALL be stored encrypted at rest and
decrypted only for the outbound fetch. The token SHALL never be serialized in
API responses or written to logs. When no encryption key is configured, PAT
storage SHALL be refused rather than stored unencrypted.

#### Scenario: Token encrypted at rest

- **WHEN** a project stores its GitHub token
- **THEN** the stored row holds only ciphertext and nonce, never the plaintext

#### Scenario: Token never echoed

- **WHEN** a project sets its GitHub token
- **THEN** the API response does not contain the token

#### Scenario: No encryption key refuses storage

- **WHEN** the server has no encryption key configured and a project attempts to
  store a token
- **THEN** the request is refused with a conflict error

### Requirement: Update-status API

The blueprints API SHALL expose the latest update status for the caller's
project: the applied-blueprint list carries update provenance fields, a POST
triggers an immediate check, a GET returns the current status, and a PUT sets the
project GitHub credential.

#### Scenario: Applied list carries update status

- **WHEN** a client requests `GET /api/blueprints/applied`
- **THEN** each entry carries `sourceUrl`, `sourceRef`, `updateAvailable`,
  `latestVersion`, and `lastCheckedAt`

#### Scenario: Trigger a check

- **WHEN** a client sends `POST /api/blueprints/:name/updates/check`
- **THEN** the server enqueues a check job and returns it

#### Scenario: Read the current status

- **WHEN** a client sends `GET /api/blueprints/:name/updates`
- **THEN** the server returns the latest check result for that blueprint name

#### Scenario: Set the project credential

- **WHEN** a client sends `PUT /api/blueprints/github-credentials` with `{token}`
- **THEN** the server stores the encrypted token and returns no content

### Requirement: User-triggered asynchronous apply

The API SHALL let a caller enqueue the application of a detected update as a
durable background job rather than performing the fetch and apply inline. A
request to `POST /api/blueprints/:name/updates/apply` SHALL create an `update`
job for the caller's project and return without waiting for it to finish.

#### Scenario: Enqueue an update

- **WHEN** a caller POSTs to `/api/blueprints/:name/updates/apply` for an
  applied, GitHub-imported blueprint
- **THEN** an `update` job is enqueued for that (project, blueprint) and the
  response reports the job as pending/running

#### Scenario: Duplicate enqueue is rejected

- **WHEN** an `update` job for the same (project, blueprint) is already
  pending or running
- **THEN** the request is rejected with `409 Conflict` and no second job is
  created

### Requirement: Update execution is idempotent and no-ops when current

Applying an update SHALL re-fetch the upstream manifest and, when the upstream
version and content are unchanged from the applied blueprint, complete
successfully as a no-op without creating a duplicate version. Otherwise it SHALL
create a new version carrying the blueprint's recorded source provenance,
publish it, and apply it to the project. Execution SHALL be idempotent under
retry.

#### Scenario: Upstream unchanged

- **WHEN** the update job runs and upstream matches the applied version/content
- **THEN** the job succeeds with a no-op result and no new version is created

#### Scenario: Newer upstream applied

- **WHEN** the update job runs and upstream is newer
- **THEN** a new blueprint version is created with the same source provenance,
  published, and applied to the project, and the job succeeds

#### Scenario: Retry after partial progress

- **WHEN** the job retries after the new version was already created (same
  name and version)
- **THEN** the existing version is reused rather than duplicated, and publish
  and apply continue

### Requirement: Update job status exposure

`GET /api/blueprints/:name/updates` SHALL return the latest `update`/`check` job
for the (project, blueprint) — its action, status
(`pending`/`running`/`succeeded`/`failed`), attempts, last error, timestamps, and
result — in addition to the current check record, so callers can follow progress
across reloads.

#### Scenario: Latest job returned

- **WHEN** a caller GETs `/api/blueprints/:name/updates` after an update was
  enqueued
- **THEN** the response includes the latest job with its status and timestamps

#### Scenario: Terminal outcome visible

- **WHEN** the update job has finished
- **THEN** the returned latest job status reflects `succeeded` or `failed` and
  carries the recorded result or error
