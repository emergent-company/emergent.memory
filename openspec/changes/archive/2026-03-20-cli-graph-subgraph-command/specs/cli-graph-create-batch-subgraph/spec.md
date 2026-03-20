## ADDED Requirements

### Requirement: create-batch accepts subgraph format
`memory graph objects create-batch` SHALL detect the input file format by inspecting the top-level JSON type and route accordingly: a JSON array routes to the existing bulk-create endpoint; a JSON object with `objects` and `relationships` keys routes to `POST /api/graph/subgraph`.

#### Scenario: flat array input unchanged
- **WHEN** `--file` points to a JSON array `[{...}, ...]`
- **THEN** behaviour is identical to the current implementation — objects are bulk-created and output is one `<entity-id>  <type>  <name>` line per object

#### Scenario: subgraph format detected and routed
- **WHEN** `--file` points to a JSON object with `objects` and `relationships` keys
- **THEN** the command POSTs to `POST /api/graph/subgraph` and prints created object count, relationship count, and exits 0

### Requirement: subgraph format uses _ref placeholders
The subgraph format SHALL support client-side `_ref` string identifiers on objects, referenced by `src_ref` / `dst_ref` on relationships, so callers never need to know real UUIDs within a single call.

#### Scenario: relationships reference objects by _ref
- **WHEN** the subgraph file defines objects with `_ref` values and relationships with matching `src_ref` / `dst_ref`
- **THEN** all objects and relationships are created atomically and the relationships are correctly wired

#### Scenario: unknown _ref in relationship
- **WHEN** a relationship references a `src_ref` or `dst_ref` not defined in the `objects` array
- **THEN** the command exits non-zero with a clear error identifying the unknown ref

### Requirement: subgraph format supports key for idempotency
Objects in the subgraph format SHALL support an optional `key` field for idempotent re-runs, consistent with `memory graph objects create --key`.

#### Scenario: keyed object already exists
- **WHEN** an object in the subgraph has a `key` and an object with that type+key already exists
- **THEN** the existing object is used (skip semantics) and the relationship is still wired correctly

### Requirement: server limits enforced with clear error
The command SHALL validate that the subgraph does not exceed 100 objects or 200 relationships before sending, and return a clear error message if exceeded.

#### Scenario: too many objects
- **WHEN** the subgraph `objects` array has more than 100 items
- **THEN** the command exits non-zero with: `subgraph exceeds limit: N objects (max 100) — split into chunks`

#### Scenario: too many relationships
- **WHEN** the subgraph `relationships` array has more than 200 items
- **THEN** the command exits non-zero with: `subgraph exceeds limit: N relationships (max 200) — split into chunks`

### Requirement: --output json returns ref_map for subgraph format
When `--output json` is passed with subgraph format input, the command SHALL print the full `CreateSubgraphResponse` JSON including the `ref_map` object mapping each `_ref` to its created entity UUID.

#### Scenario: json output includes ref_map
- **WHEN** `--output json` is passed and the input is subgraph format
- **THEN** stdout is valid JSON with `objects`, `relationships`, and `ref_map` fields

### Requirement: wrong format produces actionable error
If the top-level JSON is an array but contains relationship-shaped items (has `from`/`to` keys, no `type` matching an object type), the command SHALL exit non-zero with a message directing the user to `memory graph relationships create-batch`.

#### Scenario: relationship array passed to objects create-batch
- **WHEN** the file contains `[{"type": "depends_on", "from": "...", "to": "..."}, ...]`
- **THEN** the command exits non-zero with: `input looks like relationships — use 'memory graph relationships create-batch' instead`
