## Context

The main server repo (`emergent.memory`) hosts 879 e2e tests (57 files, 24k lines) in `apps/server/tests/e2e/` backed by `internal/testutil/`. These tests use:
- testify suites embedding `BaseSuite`
- An in-process Echo server (`server.go`) wiring all 25+ domain packages
- Per-test PostgreSQL transaction rollback for fast isolation
- Direct DB access via `bun.IDB`

The `emergent.memory.e2e` repo is the canonical e2e home. It uses:
- Standard `*testing.T` (no suites)
- `framework` package wrapping `runlog` for structured logging
- An external server only (`MEMORY_TEST_SERVER` env var)
- HTTP/CLI calls only — no direct DB access
- `t.Cleanup()` with API calls for teardown

The `internal/testutil/server.go` imports all 25+ domain packages, making it impossible to extract without pulling in the entire server. The `testutil` package cannot be cleanly separated from the main repo.

## Goals / Non-Goals

**Goals:**
- All HTTP API e2e tests live in `emergent.memory.e2e/tests/api/`
- Tests are written in the e2e repo's idiomatic style (RunLog, `*testing.T`, HTTP via `net/http`)
- Migration is phased: each phase verifies green before deleting from main repo
- After migration, `tests/e2e/` and `internal/testutil/` are fully removed from main repo
- CI job added to e2e repo; CI job removed from main repo

**Non-Goals:**
- Keeping in-process server mode in the e2e repo
- Extracting `testutil` as a shared module
- Migrating unit tests or integration tests (only `tests/e2e/`)
- Changing test logic or coverage — this is a structural migration

## Decisions

### Decision 1: Rewrite in e2e style, not port as-is
**Chosen**: Rewrite each test as a standard `*testing.T` function using RunLog and `net/http`.
**Alternatives considered**:
- Port testify suites as-is: would require copying the entire testutil + domain deps into the e2e repo — impossible without circular dependency
- Keep two repos forever: defeats the purpose, doubles maintenance burden
**Rationale**: The e2e repo style is simpler, more realistic (tests against real server), and already proven. The rewrite is mechanical — each suite method becomes a top-level function.

### Decision 2: External server only — drop in-process and transaction rollback
**Chosen**: Tests always hit an external server via HTTP. Fixture isolation via API create/delete + `t.Cleanup()`.
**Alternatives considered**:
- Keep in-process mode: requires copying 25+ domain packages into e2e repo
- Use DB truncation between tests: requires direct DB access, defeats external-server goal
**Rationale**: Transaction rollback is fast but fragile (tests can't catch real cleanup bugs). API-based teardown is slower but realistic. The added latency is acceptable for a test suite that already runs in CI against a live server.

### Decision 3: One file per domain, matching the source
**Chosen**: `tests/api/graph_test.go`, `tests/api/documents_test.go`, etc.
**Rationale**: 1:1 mapping makes PR reviews easy and allows phased deletion from main repo.

### Decision 4: Shared helpers in `tests/api/helpers_test.go`
**Chosen**: A single `helpers_test.go` per package with reusable HTTP wrappers for project/org creation, auth headers, JSON parsing, and cleanup.
**Rationale**: Mirrors the pattern used in all existing e2e packages (`tests/tools/helpers_test.go`, etc.).

### Decision 5: Phased migration — 6 phases by domain complexity
**Chosen**: Migrate low-complexity domains first, verify CI green, then delete from main repo.
**Rationale**: Reduces risk. If a phase fails, only that phase is rolled back. Main repo tests remain authoritative until e2e tests are verified.

### Decision 6: Scope-based auth tests use pre-registered test tokens
**Chosen**: Reuse the server's existing standalone test tokens (`"no-scope"`, `"read-only"`, `"graph-read"`, `"e2e-test-user"`).
**Rationale**: The standalone server already has these tokens configured in `pkg/auth/middleware.go`. The e2e server runs in standalone mode, so these tokens work without any server changes.

## Risks / Trade-offs

- **Slower teardown** — API delete instead of transaction rollback adds ~50-200ms per test. Total suite time will increase. → Mitigation: parallelize with `t.Parallel()` where tests are independent.
- **Server must be running** — unlike in-process tests, a server must be available. CI must start the server before running `tests/api/`. → Mitigation: add server startup step to CI job (already done for existing CLI tests).
- **Test token availability** — scope tests depend on server recognizing test tokens. → Mitigation: verify token support during phase 1 before migrating scope-sensitive domains.
- **API drift** — if the server API changes between main repo deletion and e2e repo addition, tests may fail to compile. → Mitigation: keep both in sync during migration (delete from main only after e2e tests pass).
- **Large PR surface** — 879 tests across 57 files is a big migration. → Mitigation: phase by phase, each phase is a separate PR.

## Migration Plan

**Phase 0 — Setup** (this change):
1. Create `tests/api/` package with `testmain_test.go` and `helpers_test.go`
2. Add CI job to e2e repo for `tests/api/`

**Phase 1** — `health`, `authinfo`, `users`, `orgs`, `projects` (~83 tests)
**Phase 2** — `apitoken`, `auth`, `useraccess`, `useractivity`, `invites`, `notifications` (~98 tests)
**Phase 3** — `documents`, `chunks`, `embedding_policies`, `extraction` (~96 tests)
**Phase 4** — `graph`, `graph_search`, `graph_analytics`, `branches`, `search` (~117 tests)
**Phase 5** — `chat`, `mcp`, `mcpregistry`, `agents`, `skills`, `schemas` (~166 tests)
**Phase 6** — `superadmin`, `tenant_isolation`, `security_scopes`, `provider` (~119 tests)

Each phase:
1. Write tests in `emergent.memory.e2e/tests/api/`
2. Run against dev server, fix failures
3. Open PR to e2e repo, CI must be green
4. Merge e2e PR
5. Open PR to main repo deleting the migrated files
6. Merge main repo PR

**Rollback**: Each phase is independent. If a phase's tests can't be made green, keep the originals in main repo and revisit.

After phase 6:
- Delete `apps/server/tests/e2e/` from main repo
- Delete `apps/server/internal/testutil/` from main repo
- Remove `test-e2e` CI job from `emergent.memory/.github/workflows/server.yml`

## Open Questions

- Should `t.Parallel()` be used within the new `tests/api/` package? (Pro: faster. Con: shared server state may cause flakiness if tests create same-named resources.)
- Should the e2e CI job for `tests/api/` use the Docker Compose stack (full server) or the shared dev server?
