## Context

The `memory graph objects list` CLI command currently supports `--type`, `--limit`, and `--label` flags. The underlying API (`GET /api/graph/objects/search`) and Go SDK (`sdkgraph.ListObjectsOptions`) already fully implement `property_filters` — a JSON-encoded array of `PropertyFilter` structs supporting operators: `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `contains`, `in`, `exists`. The only missing piece is surface-level CLI ergonomics.

## Goals / Non-Goals

**Goals:**
- Add `--filter key=value` shorthand (repeatable) to `memory graph objects list`
- Support `--filter-op` for non-equality operators: `neq`, `gt`, `gte`, `lt`, `lte`, `contains`, `in`, `exists`
- Wire parsed flags into existing `sdkgraph.ListObjectsOptions.PropertyFilters`
- Update CLI help text and the `memory-cli-reference` skill doc

**Non-Goals:**
- Changes to the API, SDK, or database layer
- Complex query expressions (OR, nested conditions) — server doesn't support them today
- JSON-literal `--properties-filter` flag (the issue proposed it but `--filter` covers all use cases more ergonomically)

## Decisions

**Decision: `key=value` shorthand, not raw JSON**
The flag `--filter status=invalidated` is readable and maps cleanly to `PropertyFilter{Path: "status", Op: "eq", Value: "invalidated"}`. Raw JSON would require quoting in shells. Alternative `--filter-json '[{"path":"status","op":"eq","value":"invalidated"}]'` is kept as a power-user escape hatch.

**Decision: `--filter-op` applies to all `--filter` values**
When `--filter-op` is set (e.g., `--filter-op contains`), every `--filter` in that invocation uses that operator. This keeps the flag surface small. For mixed operators, users can use `--filter-json`. This matches the primary use case (Nikf's examples all use a single operator per command).

**Decision: `in` operator takes comma-separated values**
`--filter status=active,draft` with `--filter-op in` passes `["active","draft"]` as the value array. Consistent with other comma-separated list flags in the CLI.

## Risks / Trade-offs

- [Risk]: `--filter` flag name conflicts if future commands add a global `--filter`. → Mitigation: scope to `graphObjectsListCmd` only; use `Flags()` not `PersistentFlags()`.
- [Risk]: Operator + value parsing may confuse users who pass `--filter 'status=a=b'`. → Mitigation: split only on first `=`; document in help text.
- [Risk]: Doc skill is a static file, may drift. → Mitigation: update it in the same PR as the code change (tracked in tasks).

## Migration Plan

No migration needed. The flag is additive; existing invocations without `--filter` are unaffected.
