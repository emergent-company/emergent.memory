## 1. Database migration

- [x] 1.1 Add `00224_add_blueprint_update_checks.sql` with `kb.blueprint_update_checks`,
      `kb.blueprint_update_jobs`, and `kb.github_credentials` (Goose Up/Down).

## 2. Encrypted GitHub PAT

- [x] 2.1 Add `GITHUB_TOKEN_ENCRYPTION_KEY` config field (fallback to
      `LLM_ENCRYPTION_KEY`); build the encryptor in `NewService`, inert when no key.
- [x] 2.2 Store/replace the encrypted PAT per project; never serialize or log it.
- [x] 2.3 Decrypt the PAT only for the outbound codeload fetch.

## 3. Update-check service + scheduler

- [x] 3.1 Register `blueprint_update_check` (daily cron) to enqueue one `check` job
      per applied GitHub-imported blueprint.
- [x] 3.2 Register `blueprint_update_jobs` (30s interval) to claim + execute jobs.
- [x] 3.3 Implement version comparison (semver where parsable, else any change) and
      canonical content comparison for same-version drift.
- [x] 3.4 Claim jobs with `FOR UPDATE SKIP LOCKED`, mark running, upsert the check
      result, and fail/requeue with exponential backoff (max 5 attempts).
- [x] 3.5 Private repos with no stored credential record a clear error without
      failing the whole tick.

## 4. API

- [x] 4.1 Extend `AppliedBlueprint` with `sourceUrl`, `sourceRef`, `updateAvailable`,
      `latestVersion`, `lastCheckedAt`.
- [x] 4.2 `POST /api/blueprints/:name/updates/check` → enqueue + return job.
- [x] 4.3 `GET /api/blueprints/:name/updates` → current status.
- [x] 4.4 `PUT /api/blueprints/github-credentials` → set/replace encrypted PAT.

## 5. CLI

- [x] 5.1 `memory blueprints check-updates` (trigger + print status).
- [x] 5.2 `memory blueprints github-token set` (flag/stdin, never echo).

## 6. Tests

- [x] 6.1 Unit: version comparator, canonical hash, crypto round-trip + inert.
- [x] 6.2 Integration: check detects update (v2) and no-update; job claim/requeue/
      backoff; credential round-trip.
- [x] 6.3 Handler/API tests for the three endpoints incl. auth.

## 7. Verification

- [x] 7.1 `go build ./...` from `apps/server`.
- [x] 7.2 `go test ./domain/blueprints/... ./domain/scheduler/...` (DB-backed) passes.
- [x] 7.3 `gofmt -l` clean on changed files.
- [x] 7.4 `openspec validate add-blueprint-update-checks --strict` passes.

## 8. Web UI surfacing

- [x] 8.1 Gateway client: applied-blueprint response fields (`sourceUrl`,
      `sourceRef`, `updateAvailable`, `latestVersion`, `lastCheckedAt`) and
      `GetBlueprintUpdates` / `CheckBlueprintUpdates` / `SetGithubCredential`.
- [x] 8.2 Gateway PRG handlers + routes: `POST /blueprints/:name/updates/check`
      and `POST /blueprints/github-credentials`.
- [x] 8.3 Installed row: repo link + ref, "Update available: vX → vY" badge,
      last-checked time, and a per-row Check-for-updates action with a pending
      state. No live Update button (Change 3).
- [x] 8.4 GitHub access token dialog (password field, never echoed).
- [x] 8.5 Gateway client/handler/render tests.
