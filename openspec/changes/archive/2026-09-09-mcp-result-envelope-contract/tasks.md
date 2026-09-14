## 1. Shared envelope helper

- [x] 1.1 Add `envelopeResult(ok bool, data map[string]any, meta map[string]any, errMsg string) *ToolResult` helper in `apps/server/domain/mcp/` (new `envelope.go` or `response_contract.go`) serializing compact JSON `{ok, error?, data, meta?}`; `error` omitted when ok, `meta` omitted when empty; verify with a unit test asserting field presence/omission for ok=true/false and empty meta
- [x] 1.2 Add registry test pinning the Phase-1 tools' envelope (entity-create, entity-update, entity-delete, relationship-create, relationship-delete, search-hybrid, search-semantic, entity-query, remember, forget each return `ok` at top level when parsed as JSON text) and verify it passes

## 2. Batch tools: entity-create / relationship-create

- [x] 2.1 Rewrite `executeBatchCreateEntities` result (service.go ~3875) to `envelopeResult(failedCount == 0, data, meta)` — `data` keeps `results[]` (per-item bool renamed `success`→`ok`), `meta` carries `created`/`failed`/`total`/`similar`/`similar_count`, drop top-level int `success`, keep `message` human-only inside `data`/`meta`; verify `mcp/entity_create_dedup_test.go` updated to decode `data`/`meta`/`ok` and passes
- [x] 2.2 Rewrite `executeBatchCreateRelationships` result (service.go ~4050) identically (no similar keys); verify related tests updated and pass
- [x] 2.3 Grep repo for readers of batch `results[].success` / top-level int `success` (agents/remember_status.go, tests, prompt strings) and confirm none outside the lockstep files in section 4 read the old shape; report the grep results

## 3. Read tools: search-hybrid / search-semantic / entity-query

- [x] 3.1 Wrap `executeHybridSearch`/`executeSemanticSearch` results (graph_tools.go ~187/233) in `envelopeResult(true, {data,total,has_more…}, verbose meta)` — slim default preserved, payload names unchanged inside `data`; verify search result tests pass
- [x] 3.2 Wrap `executeQueryEntities` (+ids fast path) result in `envelopeResult(true, {entities, projectId, pagination…})` keeping field names under `data`; verify entity-query tests pass
- [x] 3.3 Verify no `remember_status` aggregation reads search/query outputs (expect: none — search tools not aggregated); confirm with grep and note result

## 4. Consumer lockstep: agents/remember_status.go + tests

- [x] 4.1 Update `parseEntityCreate`/`parseEntityUpdate`/`parseRelationshipCreate`/`parseEntityDelete`/`parseRelationshipDelete` (remember_status.go ~315-425) to read `data.results[].ok`, `data.entity`, `data.rel`, and `meta`/`data` counts; delete the now-dead single-entity fallback branch in `parseEntityCreate`; verify `go build ./apps/server/domain/agents/` passes
- [x] 4.2 Update `remember_status_test.go` fixtures to the new envelope AND add one negative fixture feeding the OLD top-level shape asserting zero counts; verify test suite passes (goes red on stale fixtures, green on new)
- [x] 4.3 Update prompt/doc text referencing result field positions: `agents/repository.go` (~576: `pagination.total`/`has_more`/`version`/`ids[]`) and `personal_kb_agent.go` (version field) to reflect nesting under `data`; verify grep finds no stale top-level references in agent prompts
- [x] 4.4 Add `convertToolResult` passthrough test (toolpool_test.go) asserting a nested `{ok,data,meta}` envelope survives normalization with `ok`/`data`/`meta` intact and no `ok` re-injection; verify toolpool tests pass — no production change expected in toolpool.go

## 5. remember / forget structured results

- [x] 5.1 Convert `executeRemember` sync+async paths (service.go ~5250-5295) to `envelopeResult` JSON with `data` decoded from the full REST body — async `{run_id, status?, document_id?}`, sync `{run_id, status, summary, document_id}` (verify exact sync REST fields against chat/handler.go at implementation) — and verify a test decodes `data.run_id` without prose parsing (add test if none exists; currently none covers these paths)
- [x] 5.2 Convert `executeForget` sync+async paths (service.go ~5347-5391) to `envelopeResult` with `data.{run_id, status, summary}` per REST body; fix async copy "see what was created" → "see what was removed"; verify with same style of test as 5.1

## 6. Dead code + docs

- [x] 6.1 Delete unused legacy structs `CreateEntityResult`/`CreatedEntity`/`CreateRelationshipResult`/`CreatedRelationship` (mcp/entity.go ~588-634); verify `go build ./...` passes and grep confirms no references remain
- [x] 6.2 Update swagger annotations if they document affected response shapes; update `apps/server/domain/mcp/README.md` and `docs/site/` MCP pages to document the `{ok,error,data,meta}` envelope and per-item `ok`; verify grep of docs for old `results[].success`/int-`success` wording returns none
- [x] 6.3 Verify CLI (`apps/cli`) parses none of the changed result shapes (grep for entity-create/search result decoding); report finding — expected no-op

## 7. Full verification

- [x] 7.1 Run `go build ./...` from `apps/server` (or repo root per Taskfile) and confirm zero compile errors
- [x] 7.2 Run `task lint` (or `golangci-lint`) and confirm clean
- [x] 7.3 Run unit tests for affected packages: `go test ./apps/server/domain/mcp/... ./apps/server/domain/agents/...` and confirm all pass
- [ ] 7.4 Run `task test:e2e` (or the affected e2e suites: mcp/agents/remember) and confirm no regressions
- [ ] 7.5 Manual smoke via local dev server (air hot reload; `task status` health check; MCP call to entity-create + remember sync) confirming envelope shape and run_id surfacing — only if server is up; otherwise document as pending
