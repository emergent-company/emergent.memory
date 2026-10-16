## 1. Server: chunk namespace live-HEAD + default null-namespace (#1910)

- [x] 1.1 `apps/server/domain/search/repository.go`: rework `chunkNamespacePredicate` to resolve `kb.object_chunks.object_id` to the object's live HEAD (`supersedes_id IS NULL AND deleted_at IS NULL`, same `canonical_id` + branch) and test that HEAD's namespace; add the null-namespace (`o.namespace IS NULL`) branch driven by a `TextSearchParams.NamespaceIsNull` flag; fix the stale comment.
- [x] 1.2 `apps/server/domain/search/dto.go`: add `UnifiedSearchRequest.NamespaceIsNull`; `apps/server/domain/search/service.go`: forward `req.Namespace` + `req.NamespaceIsNull` into `TextSearchParams` in `executeTextSearch`.
- [x] 1.3 `apps/server/domain/mcp/graph_tools.go`: set `NamespaceIsNull` on the default (`namespace: ""`) request in `buildHybridUnifiedRequest` and the `executeSemanticSearch` path; leave `all` unfiltered.
- [x] 1.4 `apps/server/domain/search/chunk_namespace_test.go`: pin the live-HEAD/canonical resolution, the null-namespace predicate, and that `executeTextSearch` forwards the requested namespace (closes #1910(c)).
- [x] 1.5 `apps/server/domain/mcp/graph_tools_test.go`: pin that the default request sets `NamespaceIsNull` (and `all` does not).

## 2. Server: remove dead share questions endpoint (#1911a)

- [x] 2.1 Remove the `GET /api/share/agent/questions` route + `ShareHandler.ListQuestions` handler.
- [x] 2.2 Remove `ShareService.PendingQuestions`, the `ShareRepo.ListPendingQuestionsForSession` interface method, and its `share_store.go` implementation.
- [x] 2.3 Drop the `route-authority.yaml` entry and the `mock-memory.mjs` `/api/share/agent/questions` branch; remove the obsolete `share_test.go` coverage.

## 3. Server: atomic share session creation (#1911b)

- [x] 3.1 Make cap check + creation of the `kb.sessions` and `kb.agent_share_sessions` rows atomic under the existing advisory lock, so the cap path inserts nothing.
- [x] 3.2 Update the fake repo + `share_test.go` for the new signature; keep the 429 `share_session_limit` contract.
- [x] 3.3 `go build ./...` + `go test ./domain/agents/...` pass.

## 4. CI: hermetic share e2e lane (#1911c)

- [x] 4.1 Add a `share` job to `.github/workflows/web-ui.yml` (datasources shape) running `npx playwright test --config=share.config.ts`, and add it to `web-ui-ci`'s `needs`.
- [x] 4.2 Note the lane in `apps/web-ui/tests/e2e/README.md`.
- [x] 4.3 Parameterise `run-e2e.sh`'s gateway/mock ports (`MEMORY_PORT`/`MOCK_PORT`, defaults 8097/5301), derive `share.config.ts`/`datasources.config.ts` `baseURL` from them, pin disjoint job-level ports in `web-ui.yml` (`share`: 8098/5302), and add a fail-fast pre-flight guard so a lane never attaches to a sibling job's gateway + stateful mock.

## 5. Tooling: gitleaks allowlist tightening (#1912)

- [x] 5.1 Replace the whole-file path allowlist for `domain/sandbox/integration_mock_test\.go` in `.gitleaks.toml` with a value regex for `ghp_secret123`.
- [x] 5.2 `gitleaks dir .` stays green.

## 6. Verification

- [x] 6.1 `cd apps/server && go build ./...`
- [x] 6.2 `go test ./domain/search/... ./domain/mcp/... ./domain/agents/...`
- [x] 6.3 `templ generate` (if any `.templ` changed) + `go build` in `apps/web-ui/gateway`; `task lint` green for touched trees.
- [x] 6.4 `openspec validate fix-chunk-namespace-and-share-residuals` passes.
