# Design — MCP outputSchema coverage

## Context

MCP 2025-06-18 adds `structuredContent` (result) and `outputSchema` (tool declaration). PR #588 introduced both for a subset. `outputSchema` must be a JSON-Schema with an **object root**; an object schema with empty `properties` and no `additionalProperties` rejects every key.

## Fidelity strategy

Across ~109 tools with cross-domain return structs and no shared result types, per-tool accurate schemas are unmaintainable and drift. Consumers (LLMs, optional client validators) need root-type truthfulness plus a top-level hint, not a full structural contract.

Three tiers:

1. `envelopeOutputSchema()` — the existing `{ok, error, data, meta}` contract for envelope tools.
2. `objectOutputSchema()` — `{"type":"object","additionalProperties":true}`. The minimum truthful shape when top-level keys are dynamic or owned by another package.
3. `typedObjectOutputSchema(props, required)` — known stable top-level key(s), additional keys still permitted.

## Group mapping

| Group | Count | outputSchema | structuredContent |
|---|---|---|---|
| Envelope tools | 14 | `envelopeOutputSchema()` | set by `envelopeResult` |
| wrapResult / compact | 55 | `objectOutputSchema()` | set; array roots wrapped by `StructuredContentFromJSON` |
| Raw-object results | 11 | `objectOutputSchema()` (typed for `queue-reextraction`) | rerouted through `wrapResultCompact` |
| Delegated (blueprints, agents) | 18 | `objectOutputSchema()` | set by the owning domain helper |
| Array root | 1 (`session-todo-list`) | `typedObjectOutputSchema({todos: [...]})` | `{"todos": [...]}`, text unchanged |
| Prose-only allowlist | 10 | nil | nil |

## Key decisions

- **Permissive schema needs `additionalProperties`.** Adding the field to `InputSchema` is load-bearing, not cosmetic. `omitempty` keeps it out of input schemas unless set.
- **Array roots wrap, not change.** `StructuredContentFromJSON` wraps arrays in `{"results": [...]}` and primitives in `{"value": ...}` so `structuredContent` is always an object. The text block is untouched.
- **Prose results declare nothing.** Forcing `{type: object}` on markdown/text output is dishonest; MCP permits tools without `outputSchema`. These are on an explicit allowlist that the coverage test enforces.
- **Delegated tools are schemaable.** The blueprints/agents helpers produce JSON, so declaring `objectOutputSchema()` plus setting `StructuredContent` is a small fix, not an unknown.
- **Dynamic relay tools are excluded** from the coverage assertion; they forward the upstream `outputSchema` verbatim.
- **Backward compatibility.** `content[].text` remains authoritative and unchanged; additive fields are ignored by pre-2025-06-18 clients.

## Pitfalls

- Use `"type": "number"`, never `"integer"`, in typed schemas (decoded JSON numbers are `float64`).
- Error results keep prose text and do not attach a success-shaped `structuredContent`.
- Verify the browser-side MCP proxy does not drop `outputSchema` from `tools/list` before merge.
