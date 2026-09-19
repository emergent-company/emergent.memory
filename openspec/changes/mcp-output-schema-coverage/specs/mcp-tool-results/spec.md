## ADDED Requirements

### Requirement: Every structured MCP tool declares an object-root outputSchema

Every static MCP tool definition that returns a structured (JSON) result SHALL declare an `outputSchema` whose root `type` is `object`. Tools whose result is prose or markdown only SHALL be listed in an explicit prose-only allowlist and MAY omit `outputSchema`; the allowlist SHALL be enforced by a test so a new tool cannot silently omit a schema. Dynamically relayed upstream tools SHALL be exempt from the static coverage assertion and SHALL forward the upstream `outputSchema` verbatim.

#### Scenario: Structured tool exposes a declared schema in tools/list

- **WHEN** a client lists tools and inspects a structured tool's definition
- **THEN** the definition SHALL carry an `outputSchema` with `type: "object"`

#### Scenario: Prose-only tool is exempt but allowlisted

- **WHEN** a tool returns only prose or markdown (e.g. `project-get`, `call_agent`)
- **THEN** it MAY omit `outputSchema`
- **THEN** it SHALL appear in the prose-only allowlist, and a regression test SHALL fail if a non-allowlisted tool omits `outputSchema`

#### Scenario: Coverage guard fails on a silent omission

- **WHEN** a new static MCP tool is added without an `outputSchema` and without being added to the allowlist
- **THEN** the outputSchema coverage test SHALL fail

### Requirement: Declared outputSchema is truthful and permissive where the shape is unknown

A declared `outputSchema` SHALL truthfully describe the tool's result root. Where the full top-level shape is statically unknown or owned by another domain, the schema SHALL permit additional keys (`additionalProperties: true`) rather than declaring an empty `properties` object that rejects all keys. Typed schemas SHALL permit additional keys and SHALL use `number` (not `integer`) for numeric fields.

#### Scenario: Permissive schema permits unknown keys

- **WHEN** a tool's result has dynamic top-level keys and declares the permissive object schema
- **THEN** that schema SHALL set `additionalProperties: true`
- **THEN** a validator SHALL accept the tool's result

#### Scenario: Input schemas are unaffected

- **WHEN** a tool's `inputSchema` is serialized
- **THEN** it SHALL NOT emit `additionalProperties` unless explicitly set

### Requirement: structuredContent is emitted as a root object whenever the result is JSON

A tool whose result is JSON SHALL populate `structuredContent` with a root object. An object result SHALL be used as-is; an array result SHALL be wrapped in a single-key object (`{"results": [...]}`); a primitive result SHALL be wrapped as `{"value": ...}`. Prose/markdown results SHALL omit `structuredContent`. Declaring an `outputSchema` without emitting a corresponding `structuredContent` for JSON results SHALL NOT occur.

#### Scenario: Object result mirrors into structuredContent

- **WHEN** a tool returns a JSON object (via `wrapResult`, `wrapResultCompact`, or `envelopeResult`)
- **THEN** `structuredContent` SHALL be the same object

#### Scenario: Array result is wrapped

- **WHEN** a tool returns a JSON array
- **THEN** `structuredContent` SHALL be `{"results": [...]}`

#### Scenario: Array-root list keeps its text block

- **WHEN** `session-todo-list` returns a JSON array as its text block
- **THEN** the text block SHALL remain the array
- **THEN** `structuredContent` SHALL be `{"todos": [...]}`

### Requirement: The human-readable text content block is preserved

Adopting `outputSchema` and `structuredContent` SHALL be additive. The `content[].text` block SHALL remain the authoritative human-readable payload and SHALL NOT change as a result of declaring a schema or populating `structuredContent`.

#### Scenario: Text contract unchanged

- **WHEN** a tool that previously returned a text payload is called after this change
- **THEN** `content[0].text` SHALL be byte-identical to the previous output
