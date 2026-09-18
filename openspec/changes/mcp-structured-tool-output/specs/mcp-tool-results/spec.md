## ADDED Requirements

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

For every tool that returns `structuredContent`, the server SHALL continue to return the equivalent human-readable payload in the text `content` block (MCP's backward-compatibility SHOULD). The serialized text JSON SHALL be identical to the `structuredContent` object, so existing text-parsing consumers are unaffected.

#### Scenario: Text and structured content are equivalent

- **WHEN** a client calls an envelope tool
- **THEN** the `content` array SHALL still contain a text block whose JSON is byte-equivalent to `structuredContent`

### Requirement: isError stays reserved for call failure

`isError` SHALL remain reserved for cases where the tool call could not be performed (protocol-level failures such as an unknown tool or invalid invocation), and SHALL NOT be set merely because the tool reported a negative or failed business result. A failed business result SHALL be conveyed through the envelope (`ok: false` + `error`), not through `isError`.

#### Scenario: Business failure does not set isError

- **WHEN** an envelope tool completes but reports `ok: false` (e.g. a batch that partially failed)
- **THEN** `isError` SHALL be absent (or `false`) and the failure SHALL be visible in `structuredContent.error`
