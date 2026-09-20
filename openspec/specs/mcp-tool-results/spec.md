# mcp-tool-results Specification

## Purpose
Defines a uniform, machine-classifiable result envelope for every MCP tool exposed by the Memory server: bool `ok` status, optional `error`, tool-specific `data`, and optional `meta`, so consumers (chat UIs, agent executors, MCP clients) can classify success/failure and extract results generically without per-tool heuristics.

## Requirements

### Requirement: MCP tool results use a uniform envelope
Every MCP tool that returns a structured result SHALL return a single top-level JSON object of the form `{ "ok": <bool>, "error": <string>, "data": <tool-specific>, "meta": <object> }`. `ok` SHALL be `true` when the operation succeeded and `false` when it failed. `error` SHALL be present and non-empty exactly when `ok` is `false`, and SHALL be omitted when `ok` is `true`. `data` SHALL carry the tool-specific payload. `meta` SHALL be present only when there is non-payload metadata to report (counts, pagination, deprecation notices). Existing tool-specific payload field names SHALL be preserved inside `data` (search keeps `data`/`total`/`has_more`, entity-query keeps `entities`/`pagination`, batch tools keep `results`), so only the wrapping layer changes. Tools newly added to the MCP server SHALL use the shared envelope helper.

#### Scenario: Successful entity-create call
- **WHEN** a client calls `entity-create` with valid inputs that all succeed
- **THEN** the result SHALL be a JSON object with `ok: true`
- **THEN** the result SHALL NOT contain an `error` field
- **THEN** the created-entity details SHALL appear inside `data`
- **THEN** numeric batch counts SHALL appear in `meta` (or `data`), not as the status field

#### Scenario: Failed entity-create call
- **WHEN** a client calls `entity-create` and some or all items fail
- **THEN** the result SHALL be a JSON object with `ok: false` when any item failed
- **THEN** `error` SHALL be present with a machine-readable description

#### Scenario: Partial batch failure is still a failure
- **WHEN** a batch tool (e.g. `entity-create`, `relationship-create`) succeeds for some items and fails for others
- **THEN** top-level `ok` SHALL be `false`
- **THEN** per-item outcome SHALL be available in `data.results` so a client can identify which items failed and why

#### Scenario: New tool uses the envelope helper
- **WHEN** a new MCP tool returning structured results is added to the server
- **THEN** its results SHALL be produced through the shared envelope helper (registry test covers the Phase-1 tools; convention governs new ones)

### Requirement: `success` is a boolean status, not a numeric count
No MCP tool result SHALL use a numeric-typed field named `success`. Status SHALL be carried by boolean `ok`. Numeric outcomes (created/failed/total counts) SHALL use distinct numeric field names (`created`, `failed`, `total`, or equivalents in `meta`/`data`) that never collide with the boolean status field. Per-item status exposed inside `data.results` arrays SHALL be a boolean named `ok`.

#### Scenario: Batch result exposes distinct count fields
- **WHEN** a batch tool returns numeric counts
- **THEN** counts SHALL appear under names such as `created`, `failed`, `total` (numeric)
- **THEN** no count SHALL be serialized under the field name `success`

#### Scenario: Per-item status is a boolean named ok
- **WHEN** a batch tool exposes per-item outcome inside `data.results`
- **THEN** each item's status SHALL be a boolean under the key `ok`
- **THEN** a client SHALL NOT need to special-case a per-item `success` count

#### Scenario: Client distinguishes zero successes from failure
- **WHEN** a batch tool completes with zero created items and no failures
- **THEN** a client reading `ok`/`created`/`failed` SHALL be able to tell "nothing created" apart from "operation failed" without per-tool knowledge

### Requirement: `message` is a human summary, never a status signal
Any `message` field on an MCP tool result SHALL contain human-readable prose only and SHALL NOT be used by clients to determine success or failure. `message` MAY be present on both successful and failed results. Status SHALL be determined solely from `ok`/`error`.

#### Scenario: Successful result still carries a message
- **WHEN** a batch tool succeeds and returns a summary `message`
- **THEN** the result SHALL have `ok: true` regardless of the non-empty `message`

### Requirement: `remember` and `forget` results expose structured run metadata
The `remember` and `forget` MCP tools SHALL return structured results (not bare prose) that include `run_id` and any completion data the tool knows at return time (e.g. status, document id, summary, created/changed counts). The returned `run_id` SHALL be usable directly as the input to `remember-status`. A client SHALL NOT need to parse prose to obtain the `run_id`.

#### Scenario: Sync remember returns its run_id
- **WHEN** a client calls `remember` with `mode: sync` and it completes
- **THEN** the result SHALL contain a structured `run_id` field
- **THEN** the client SHALL be able to pass that `run_id` to `remember-status` without text parsing

#### Scenario: Sync forget returns its run_id
- **WHEN** a client calls `forget` with `mode: sync` and it completes
- **THEN** the result SHALL contain a structured `run_id` field

#### Scenario: Async remember/forget still returns run_id
- **WHEN** a client calls `remember` or `forget` with an async/stream mode
- **THEN** the returned result SHALL contain the `run_id` of the started run
- **THEN** a client SHALL be able to poll `remember-status` with that `run_id`

### Requirement: Read and search tools return slim entities by default
Read-oriented MCP tools (`search-hybrid`, `search-semantic`, `entity-query`, and similar) SHALL return slim entity records by default, exposing only fields useful to agents and UIs (`id`, `type`, `key`, `name`, `properties` as applicable) and SHALL NOT emit verbose internal fields (`canonical_id`, `project_id`, `version_id`, unnecessary `created_at`/`updated_at`, `score` inside the object, nested pagination blobs) in the default projection. Full records MAY be returned when the caller passes an explicit `verbose`/`include_meta` flag. When a search returns scores, `score` SHALL appear at the result-item level, not nested inside the entity object.

#### Scenario: Default search-hybrid response is slim
- **WHEN** a client calls `search-hybrid` without a verbose flag
- **THEN** each returned entity SHALL contain only the slim field set
- **THEN** the entity object SHALL NOT contain internal fields like `canonical_id` or `project_id`
- **THEN** any relevance `score` SHALL be a sibling of the entity object, not inside it

#### Scenario: Verbose search returns full records
- **WHEN** a client calls a read tool with `verbose: true` (or equivalent)
- **THEN** the result SHALL include the fuller entity records including internal identifiers and timestamps

### Requirement: In-process consumers parse results via the same contract
Any in-process consumer that interprets tool results for status or counts (agent-run `remember-status` aggregation, tool-result normalization for executors) SHALL read the uniform envelope (`ok`/`error`/`data`/`meta`) rather than legacy field positions, so a single producer change cannot silently break downstream counts.

#### Scenario: Agent remember-status counts reflect envelope results
- **WHEN** an agent run performs `entity-create` calls and `remember-status` aggregates them
- **THEN** created-entity counts SHALL be derived from the current envelope shape (`data.results[].ok`, `data.entity`, `meta` counts)
- **THEN** a result in the legacy top-level shape (`results[]` with per-item `success`) SHALL NOT be counted, and a regression test SHALL pin that behavior

#### Scenario: Executor normalization round-trips the envelope
- **WHEN** a tool result in the uniform envelope passes through executor normalization (`convertToolResult`)
- **THEN** `ok`, `data`, and `meta` SHALL be preserved intact (no flattening of `data` into the top level)

### Requirement: Envelope tools declare an output schema

Every MCP tool whose result is produced by the shared envelope helper SHALL declare an `outputSchema` in its `tools/list` entry. The schema SHALL be a JSON Schema rooted at `type: "object"` with `properties` for `ok` (boolean), `error` (string), `data` (object), and `meta` (object), and SHALL mark `ok` and `data` as `required`. Tools that return prose or non-envelope payloads SHALL NOT declare an `outputSchema`.

#### Scenario: Envelope tool lists its output schema

- **WHEN** a client calls `tools/list`
- **THEN** each envelope tool (e.g. `entity-create`, `search-hybrid`, `remember`) SHALL include an `outputSchema` field with `type: "object"` and `required` containing `ok` and `data`

#### Scenario: Non-envelope tool omits output schema

- **WHEN** a client calls `tools/list`
- **THEN** a prose or wrapResult-backed tool SHALL NOT include an `outputSchema` field

### Requirement: Envelope tools return structuredContent mirroring the text

For every envelope tool, the server SHALL return a `structuredContent` field in the `tools/call` result containing the same JSON object that is serialized into the text `content` block. `structuredContent` SHALL be a root JSON object whose keys match the envelope (`ok`, `data`, and optionally `error`/`meta`), so a programmatic consumer can read the same values without parsing prose.

#### Scenario: Successful call returns structured content

- **WHEN** a client calls an envelope tool successfully
- **THEN** the result SHALL contain a `structuredContent` object whose `ok` is `true`
- **THEN** `structuredContent.data` SHALL hold the same tool payload as the text block

#### Scenario: Failed call returns structured content

- **WHEN** a client calls an envelope tool that fails
- **THEN** the result SHALL contain a `structuredContent` object whose `ok` is `false` and `error` is set

### Requirement: Text content block remains for backward compatibility

For every tool that returns `structuredContent`, the server SHALL continue to return the equivalent human-readable payload in the text `content` block (MCP's backward-compatibility SHOULD). The serialized text JSON SHALL encode the same JSON object (semantically equal) as the `structuredContent` object, so existing text-parsing consumers are unaffected.

#### Scenario: Text and structured content are equivalent

- **WHEN** a client calls an envelope tool
- **THEN** the `content` array SHALL still contain a text block whose JSON encodes the same JSON object as `structuredContent`

### Requirement: isError stays reserved for call failure

`isError` SHALL remain reserved for cases where the tool call could not be performed (protocol-level failures such as an unknown tool or invalid invocation), and SHALL NOT be set merely because the tool reported a negative or failed business result. A failed business result SHALL be conveyed through the envelope (`ok: false` + `error`), not through `isError`.

#### Scenario: Business failure does not set isError

- **WHEN** an envelope tool completes but reports `ok: false` (e.g. a batch that partially failed)
- **THEN** `isError` SHALL be absent (or `false`) and the failure SHALL be visible in `structuredContent.error`

### Requirement: Read tools bound emitted property value size

Read/search MCP tools that emit entity or relationship `properties` SHALL bound the size of each emitted property value so a single oversized field (e.g. a full document `content` blob) cannot inflate a tool result and overflow the caller's context. Any string property value longer than a server-side cap SHALL be truncated in the emitted result with an explicit truncation marker, and the truncation SHALL apply equally under the `full` field strategy and inside nested maps/arrays. Property values under the cap SHALL be emitted unchanged.

#### Scenario: Oversized string property is truncated
- **WHEN** a read/search tool returns an entity whose `properties` contains a string value longer than the cap
- **THEN** the emitted value SHALL be truncated to the cap
- **THEN** a truncation marker SHALL indicate how many characters were omitted

#### Scenario: Truncation applies under the full strategy
- **WHEN** a caller requests `field_strategy: "full"` and a result has an oversized string property
- **THEN** the emitted `properties` value SHALL still be truncated

#### Scenario: Short and non-string properties are unchanged
- **WHEN** every property value is under the cap
- **THEN** the emitted properties SHALL be unchanged
- **AND** non-string property values SHALL be passed through unchanged
