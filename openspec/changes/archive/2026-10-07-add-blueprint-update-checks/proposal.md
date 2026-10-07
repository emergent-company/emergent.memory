## Why

Blueprints imported from GitHub now record their source provenance (Change 1:
`source_kind`/`source_url`/`source_ref`), but nothing detects when the upstream
repository publishes a newer version. A project with an applied GitHub blueprint
has no signal that an update exists. Change 2 adds the detection half of
update-tracking: a periodic, always-on check that compares each applied,
GitHub-imported blueprint against its upstream source, plus the encrypted PAT
storage, durable job queue, and API/CLI surface to trigger and read those checks.
The user-triggered apply/update execution is deferred to Change 3.

## What Changes

- Two new tables: `kb.blueprint_update_checks` (per-project/blueprint result)
  and `kb.blueprint_update_jobs` (durable `check`/`update` job queue), plus
  `kb.github_credentials` (encrypted project PAT).
- A blueprints-domain update-check service registered on the scheduler: a daily
  sweep enqueues one `check` job per applied GitHub-imported blueprint, and a
  30s interval runner claims jobs (`FOR UPDATE SKIP LOCKED`, multi-instance
  safe), fetches the upstream manifest through the SSRF-guarded fetcher,
  compares version + content, and upserts the result with retry/backoff.
- Encrypted PAT storage reusing `pkg/crypto`; key from
  `GITHUB_TOKEN_ENCRYPTION_KEY` falling back to `LLM_ENCRYPTION_KEY`; inert when
  unset. The token is never serialized or logged.
- API: extend the applied-blueprint list with update status; `POST`/`GET`
  `/api/blueprints/:name/updates(/check)`; `PUT`
  `/api/blueprints/github-credentials`.
- Web UI: surface the update status on the blueprints page — the upstream repo
  and ref on each GitHub-imported row, an "Update available: vX → vY" badge with
  the last-checked time, a per-row "Check for updates" action, and a dialog to
  store the project GitHub access token. The Update (apply) action itself is
  deferred to Change 3.
- CLI: `memory blueprints check-updates` and `memory blueprints github-token set`.

Out of scope: executing the apply/update (Change 3).

## Capabilities

### Modified Capabilities

- `blueprint-api`: provenance-based update detection, the periodic check, the
  durable job queue with retry/backoff, encrypted project PAT, and the
  update-status API.

## Impact

- **Server** (`apps/server`): migration `00224`, `domain/blueprints` entities,
  repository, service (update-check + credential logic), handler, routes, module
  wiring, and `internal/config` (encryption key + schedules).
- **CLI** (`apps/cli`): two new `blueprints` subcommands.
- **Web UI** (`apps/web-ui`): gateway client methods + PRG handlers/routes for
  the update check and GitHub credential, and the blueprints-page surfaces
  (source line, update badge, check action, token dialog).
- **Tests**: unit (version comparator, canonical hash, crypto) + DB-backed
  integration (check detects/no-update, job claim/backoff, credential
  round-trip) + handler/API tests + gateway client/handler/render tests.
