## Why

The `scopeKey` read path in `entity-query` (`enforceEntityQueryScope`) treated a
malformed/unparseable declaration as absent and warned instead of failing. That
was accepted in #1176 only because "all write paths validate" — but two write
paths do not:

- **`schema-create` (MCP tool)** writes `kb.project_object_schema_registry`
  `json_schema` directly, bypassing `CreatePack`/`UpdatePack`.
- **Backup restore** inserts `json_schema` verbatim from the snapshot.

So a malformed declaration can still enter the registry, after which
`entity-query` silently ignores it (fails open) — the exact silent
cross-document scan #1148 was meant to prevent. Closes #1177.

## What Changes

- **Write-path validation.** The existing scope-key validator is applied to both
  bypass paths, fail-closed with an actionable message, consistent with
  `CreatePack`/`UpdatePack`:
  - `schema-create` MCP tool validates every object type's `scopeKey` before it
    inserts any registry row.
  - Backup restore validates each schema row's `json_schema` before inserting
    `kb.object_type_schemas` / `kb.project_object_schema_registry`.
- **Read-path policy: caller-visible rejection.** A declaration that cannot be
  parsed (not a JSON object) or is present but omits the required `property` is
  no longer treated as absent. A **filtered** `entity-query` for such a type is
  rejected caller-visibly (`fail-closed`) with an actionable error naming the
  type, instead of warn-and-continue with an unscoped query. The trade-off is
  deliberate and now explicit in the spec: after the write-path fix this only
  affects a legacy/tampered row, and rejecting the filtered query is safer than
  silently running it unscoped. Queries with no `filters` are unaffected.
- The `object-type-scope-key` and `mcp-tool-results` requirements are tightened
  via delta specs; the canonical specs are not edited directly.

### Alternatives considered

- **Keep warn-and-continue.** Rejected: it leaves the #1148 silent cross-document
  scan reachable through a legacy row, which is precisely the failure this
  change exists to close. A caller-visible rejection is diagnosable and
  self-healing (fix or remove the declaration).

## Capabilities

### Modified Capabilities

- `object-type-scope-key`: extend "validation fails closed" to the `schema-create`
  MCP tool and backup restore.
- `mcp-tool-results`: `entity-query` rejects a filtered query caller-visibly when
  the type's `scopeKey` is unparseable or missing its property.

## Impact

- `apps/server/domain/schemas/scope_key.go` — new
  `ValidateTypeSchemaScopeKeys` helper (validates a type-name → schema set with
  a locally derived known-type map).
- `apps/server/domain/mcp/schema_tools.go` — validate before inserting in
  `executeCreateSchema`.
- `apps/server/domain/mcp/service.go` — `enforceEntityQueryScope` fails closed
  with a caller-visible error on a malformed/empty-property declaration.
- `apps/server/domain/backups/restorer.go` — validate schema rows in
  `insertTableRows` before writing.
- Tests: fail-first DB-backed tests for the `schema-create` rejection, the
  restore rejection, and the read-path rejection; a valid-declaration
  non-regression case.
- No migration. No API/response shape change. `key_prefix`, alias handling,
  `null`-as-absent and the no-declaration behaviour are unchanged.
