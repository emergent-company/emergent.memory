## Why

Residuals left by the chunk-namespace scoping work (#1902/#1907 → #1910),
the share-surface review (#1906 → #1911), and the gitleaks allowlist tightening
#1905 landed (→ #1912). Each is a small, self-contained defect that is cheap to
fix now and expensive to leave:

- **#1910 — chunk namespace scoping still leaks.** The chunk namespace predicate
  requires only `o.deleted_at IS NULL`, but deletes are *versioned*: `SoftDelete`
  inserts a tombstone HEAD and `CreateVersion` sets `supersedes_id` on the prior
  HEAD, leaving prior rows' `deleted_at` NULL. A soft-deleted object's chunks are
  therefore still admitted, contradicting both the code comment and the spec
  scenario. Separately, a *default* search (`namespace: ""`) with
  `include_chunks: true` still returns chunks extracted into non-null-namespace
  objects: `mapUnifiedToSearchResponse` exempts `text_chunk` from the
  namespace-derived allow/block sets, and no chunk predicate applies when
  `Namespace == nil` — while `getSystemNamespaceTypes` blocks non-null-namespace
  *types* from default searches. And the exact line #1902 broke
  (`executeTextSearch` → `TextSearchParams.Namespace`) has no test: deleting it
  keeps the suite green.
- **#1911 — share residuals.**
  - `GET /api/share/agent/questions` is dead: its gateway client was removed in
    #1893 and nothing consumes the server endpoint.
  - `ShareService.CreateSession` inserts the backing `kb.sessions` row *before*
    `CreateShareSessionIfUnderCap`; on the cap path (`created=false` → 429) the
    session row is orphaned forever.
  - `apps/web-ui/tests/e2e/share.config.ts` (hermetic mock harness) is not in any
    CI lane, so `agent-public-share.spec.ts` never runs in CI.
- **#1912 — gitleaks allowlist too wide.** `.gitleaks.toml` waives the *whole*
  `domain/sandbox/integration_mock_test\.go` file, so any real secret added to
  that file later would be silently ignored. The other two entries added at the
  same time are tight value/name regexes.

## What Changes

### #1910 — chunk namespace scoping

- `chunkNamespacePredicate` (`apps/server/domain/search/repository.go`) now
  resolves the version linked by `kb.object_chunks.object_id` to the object's
  **live HEAD** — the `supersedes_id IS NULL AND deleted_at IS NULL` row of the
  same `canonical_id` and branch — and tests that HEAD's namespace. A chunk
  linked to a superseded-but-still-live version still counts (namespace is
  inherited across versions); a chunk whose object has been soft-deleted (its
  HEAD is a tombstone) is now excluded. The stale comment is corrected.
- A default (null-namespace) chunk predicate is applied when the MCP default
  search omits a namespace: `search.TextSearchParams` and
  `search.UnifiedSearchRequest` gain a `NamespaceIsNull` flag, the MCP
  `search-hybrid`/`search-semantic` request builders set it for
  `namespace: ""` (not for `all`), and `executeTextSearch` forwards it. Chunks
  extracted into non-null-namespace objects no longer leak into a default search.
- Tests: `chunk_namespace_test.go` pins the live-HEAD/canonical resolution, the
  null-namespace predicate, and — closing #1910(c) — that
  `executeTextSearch` forwards the requested namespace into `TextSearchParams`.

### #1911 — share residuals

- **Remove** the dead `GET /api/share/agent/questions` surface: the route, the
  `ShareHandler.ListQuestions` handler, `ShareService.PendingQuestions`, the
  `ShareRepo.ListPendingQuestionsForSession` interface method + its store
  implementation, the `route-authority.yaml` entry, the `mock-memory.mjs`
  branch, and the now-obsolete unit test.
- **Fix** `ShareService.CreateSession` so the backing `kb.sessions` row and the
  share-session row are created atomically under the existing advisory lock +
  count: the cap check runs *first*, and when the cap is reached neither row is
  inserted — no orphaned `kb.sessions` row. The `created=false` → 429 contract is
  unchanged.
- **Wire** the hermetic `share.config.ts` suite into CI as a new `share` job in
  `.github/workflows/web-ui.yml` (same shape as the `datasources` job), and add
  it to the `web-ui-ci` aggregate.

### #1912 — gitleaks allowlist

- Replace the whole-file path allowlist for
  `domain/sandbox/integration_mock_test\.go` with a value regex for the literal
  fabricated token (`ghp_secret123`), matching the token-shape / env-var-name
  entries added alongside it. `gitleaks dir .` stays green.

## Impact

- `apps/server/domain/search/repository.go` — predicate + `TextSearchParams`.
- `apps/server/domain/search/service.go` — forward the namespace + null-namespace flag.
- `apps/server/domain/search/dto.go` — `UnifiedSearchRequest.NamespaceIsNull`.
- `apps/server/domain/mcp/graph_tools.go` — default-search request builders.
- `apps/server/domain/search/chunk_namespace_test.go`, `apps/server/domain/mcp/graph_tools_test.go` — tests.
- `apps/server/domain/agents/share_handler.go`, `share_service.go`, `share_store.go`, `share_test.go` — endpoint removal + atomic cap.
- `apps/server/route-authority.yaml` — drop the dead route.
- `apps/web-ui/tests/e2e/mock-memory.mjs` — drop the dead mock branch.
- `.github/workflows/web-ui.yml`, `apps/web-ui/tests/e2e/README.md` — share e2e CI lane.
- `.gitleaks.toml` — tight value regex.
