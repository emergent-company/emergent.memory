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
