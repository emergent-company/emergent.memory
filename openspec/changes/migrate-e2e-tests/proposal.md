## Why

The `emergent.memory` main repo contains 879 e2e tests across 57 files in `apps/server/tests/e2e/` that test HTTP API behavior using an in-process server and direct DB access. The `emergent.memory.e2e` repo has a more mature, production-realistic test suite (RunLog framework, external server, CLI-first) that better reflects real deployment scenarios. Consolidating all e2e testing into one place reduces duplication, improves test realism, and makes it easier to run tests against any environment (local, dev, staging, production).

## What Changes

- New `tests/api/` package added to `emergent.memory.e2e` with 57 test files rewritten in the e2e repo style (standard `*testing.T`, RunLog, HTTP calls via `net/http`, no testify suites)
- All 879 server e2e tests migrated domain-by-domain across 6 phases
- After each phase is verified green, corresponding test files deleted from `emergent.memory/apps/server/tests/e2e/`
- After full migration: `apps/server/tests/e2e/` and `apps/server/internal/testutil/` removed from main repo
- CI job `test-e2e` removed from `emergent.memory/.github/workflows/server.yml`
- CI job added to `emergent.memory.e2e` for the new `tests/api/` package

## Capabilities

### New Capabilities

- `api-e2e-suite`: A new `tests/api/` test package in `emergent.memory.e2e` that covers all HTTP API endpoints using RunLog, standard `*testing.T`, and an external server — replacing the in-process server e2e tests in the main repo.

### Modified Capabilities

<!-- No existing spec-level requirement changes -->

## Impact

- `emergent.memory/apps/server/tests/e2e/` — 57 files removed (phased)
- `emergent.memory/apps/server/internal/testutil/` — removed after migration complete
- `emergent.memory/.github/workflows/server.yml` — `test-e2e` job removed
- `emergent.memory.e2e/tests/api/` — new package (~57 files, 879 tests)
- `emergent.memory.e2e/.github/workflows/` — new CI job for `tests/api/`
- No production code changes; no API changes; no breaking changes
