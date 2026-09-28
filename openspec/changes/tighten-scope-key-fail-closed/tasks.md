## 1. Shared validator

- [x] 1.1 Add `ValidateTypeSchemaScopeKeys(map[string]json.RawMessage)` in `domain/schemas` (deterministic, derives the known-type map from the same set).

## 2. Write paths fail closed (issue #1177)

- [x] 2.1 `schema-create` MCP tool (`executeCreateSchema`): validate every object type's `scopeKey` before inserting any `graph_schemas` / registry row.
- [x] 2.2 Backup restore (`insertTableRows`): validate `object_type_schemas` and `project_object_schema_registry` rows before writing.

## 3. Read-path policy

- [x] 3.1 `enforceEntityQueryScope`: reject a filtered query caller-visibly when the declaration is unparseable or omits `property` (no warn-and-continue).
- [x] 3.2 Keep absent / `null` / no-declaration behaviour unchanged.

## 4. Spec

- [x] 4.1 `object-type-scope-key` delta: extend the fail-closed validation requirement to the two bypass write paths.
- [x] 4.2 `mcp-tool-results` delta: add the unparseable-declaration rejection and record the availability trade-off explicitly.

## 5. Tests (fail-first, DB-backed)

- [x] 5.1 (a) `schema-create` with a malformed `scopeKey` → rejected, nothing persisted (RED before the fix).
- [x] 5.2 (b) backup restore with a malformed `scopeKey` → rejected, nothing persisted (RED before the fix).
- [x] 5.3 (c) existing malformed registry row + filtered query → caller-visible rejection (RED before the fix); valid declaration still persists.
- [x] 5.4 No-regression: full `schemas`, `backups`, `mcp` DB test packages pass.

## 6. Verification

- [x] 6.1 `go build ./...`, `task lint`, `gofmt -l`.
- [x] 6.2 `bash scripts/preflight/all.sh` (skip-census at/below baseline).
- [x] 6.3 `openspec validate --all --strict`.
