## 1. Single source of truth

- [x] 1.1 Export the canonical relation as `auth.ScopeImplies` (keys/values byte-identical) and add `auth.ExpandScopes` as the single expansion entry point; verify `go build ./...` passes
- [x] 1.2 Delete `domain/mcp.scopeImpliesMap` and derive the MCP view from `auth.ExpandScopes`; verify `go build ./...` passes
- [x] 1.3 Confirm no import cycle and record the layering justification in `design.md`

## 2. MCP projection and agreement tests

- [x] 2.1 Add `mcpToolScopeVocabulary` derived from the tool catalog (static scope map + package-level dynamic tool builders)
- [x] 2.2 Project `auth.ExpandScopes` onto the vocabulary in `expandScopesSet`, retaining explicit scopes verbatim
- [x] 2.3 Add a derivation-identity test asserting the MCP view equals the canonical projection for a representative input battery, and verify `TestMCPExpansionIsCanonicalProjectedOntoToolScopes` passes
- [x] 2.4 Pin the exact tool-visible expanded set per umbrella scope, and verify `TestMCPExpansionPinnedToolVisibleSets` passes
- [x] 2.5 Assert no expansion reaches an excluded family (`admin:read`/`admin:write`, `mcp:admin`, `org:*`, `project:invite:create`, `account:*`), and verify `TestMCPExpansionNeverReachesExcludedFamilies` passes
- [x] 2.6 Assert vocabulary completeness against the tool catalog, and verify `TestMCPToolScopeVocabularyCoversCatalog` passes

## 3. Reconciliation

- [x] 3.1 Add `TestMCPExpansionReconcilesDataWriteToSchemaWrite` documenting `data:read → schema:read`, `data:write → schema:write`, and single-level (no `data:write → schema:migrate`)
- [x] 3.2 Record the security analysis (what widens, bounded families, parity with REST) in `design.md`
- [x] 3.3 Confirm the reconciliation does not change #803's pinned role expansion (viewer 17 / user 29 / admin 33)

## 4. Spec

- [x] 4.1 Add the `unify-scope-implications` OpenSpec change with delta specs for the single source of truth and the MCP tool-surface projection
- [x] 4.2 `openspec validate --strict` passes for the change

## 5. Verification

- [x] 5.1 `go build ./...` in `apps/server` passes
- [x] 5.2 `go test -count=1 ./pkg/auth/... ./domain/mcp/...` passes, including the DB-backed MCP tests via `TEST_DATABASE_URL` + `REQUIRE_DB=1`
- [x] 5.3 `golangci-lint run --new-from-rev=origin/main ./pkg/auth/... ./domain/mcp/...` reports 0 issues and `gofmt -l` is clean on the changed files

## 6. Out of scope

- [ ] 6.1 The wider "one authority for scopes" (Zitadel authenticates, app authorizes) refactor — separate design.
- [ ] 6.2 Making `domain/agents/toolgroups`' hand-maintained dynamic-scope list consume the catalog-derived vocabulary — follow-up.
