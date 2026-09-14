## Context

The `memory` CLI wraps the Memory platform API and provides commands across `documents`, `graph`, `schemas`, `projects`, etc. The server exposes a full extraction job management API at `/api/admin/extraction-jobs` (create, get, list, cancel, retry, logs, stats, bulk operations) behind `admin:read` / `admin:write` token scopes.

Currently zero extraction job commands exist in the CLI. E2e tests and operators must use raw HTTP to trigger, poll, and inspect jobs. The `documents extraction-summary` command provides a narrow document-centric view of the last completed job, but nothing for in-progress job management.

## Goals / Non-Goals

**Goals:**
- Add `memory extraction jobs` command group covering the full admin API surface needed for day-to-day operations and testable workflows
- Commands: `create`, `get`, `list`, `cancel`, `retry`, `logs`, `stats`
- Support `--output json` on all commands for machine-readable output
- Replace raw HTTP helpers in the e2e tests with CLI calls

**Non-Goals:**
- Bulk operations (`bulk-cancel`, `bulk-delete`, `bulk-retry`) — admin-level maintenance; out of scope for initial CLI
- A new SDK package — the CLI will call the admin HTTP API directly using the existing `client.Client` auth infrastructure (same pattern as `tokens.go` which does raw HTTP)
- GUI/TUI for job monitoring
- Streaming log output (logs are returned as a snapshot)

## Decisions

### 1. New top-level `extraction` command, not under `documents`

**Decision**: `memory extraction jobs <subcommand>` rather than `memory documents extraction-jobs <subcommand>`.

**Rationale**: Extraction jobs are not document-specific — they can run on other source types in the future. A top-level `extraction` group with a `jobs` subgroup mirrors how `graph` has `objects` and `relationships`. It also leaves room for `memory extraction schemas` etc.

**Alternative considered**: Hang everything under `documents` as `memory documents jobs`. Rejected because it couples a multi-source concept to documents and pollutes the documents namespace.

### 2. Raw HTTP in CLI (no new SDK package)

**Decision**: Implement the API calls directly in `extraction.go` using the `http.Client` from the existing `client.Client.SDK.HTTPClient()` or by constructing requests with the token retrieved via `getClient(cmd)`.

**Rationale**: The superadmin SDK (`pkg/sdk/superadmin`) already has partial extraction job types but targets a different endpoint (`/api/superadmin/...`). The admin SDK (`/api/admin/extraction-jobs`) has no dedicated SDK package. Creating one is out of scope. Other CLI commands (`tokens.go`, `traces.go`) already call HTTP directly — this is an established pattern.

**Alternative considered**: Create `pkg/sdk/extractionjobs/client.go`. Deferred — can be done as a follow-up if the SDK gains wider use.

### 3. `GroupID: "knowledge"` for the extraction command

**Decision**: Register under the `knowledge` group in root (same as `documents`, `graph`, `schemas`).

**Rationale**: Extraction jobs are knowledge-base operations. Consistent with existing grouping.

### 4. Output format: table default, `--output json` opt-in

**Decision**: Default to human-readable table output; `--output json` emits the raw API response.

**Rationale**: Consistent with every other command in the CLI.

## Risks / Trade-offs

- [Risk] Admin token scope (`admin:read` / `admin:write`) required — project tokens will get 403. → Mitigation: return a clear error message directing users to use an account-level API key; document in command `Long` help.
- [Risk] `waitForExtractionJob` e2e helper replaced with CLI — adds one process-spawn per poll tick (every 5s). → Mitigation: acceptable for e2e tests; the poll interval is already 5s.
- [Risk] Status string mismatch between server DTO and CLI switch — if server adds a new status value the CLI silently falls through. → Mitigation: default case in table rendering prints the raw status string.

## Migration Plan

1. Add `extraction.go` to `apps/cli/internal/cmd/`
2. Register `extractionCmd` in `root.go` `init()`
3. Update e2e test helpers: `triggerExtractionJob` → `mustRunCLIInDirWithHome(..., "extraction", "jobs", "create", ...)`, `waitForExtractionJob` → poll loop using `runCLIInDirWithHome(..., "extraction", "jobs", "get", jobID, "--output", "json")`
4. Add small PDF fixture to `tests/cli/testdata/` and new `TestCLIInstalled_DocumentConversion` test
5. Build and smoke-test locally; run `runlog test local-fixed TestCLIInstalled_DocumentExtractionWithSchema`

No server changes. No migration or rollback needed — adding commands is additive.

## Open Questions

- Should `memory extraction jobs create` accept `--wait` to block until the job completes? Useful for scripting. Can be added in a follow-up.
- Should `memory extraction jobs list` be accessible with a project token (if the server relaxes the `admin:write` scope)? No change needed in CLI; depends on server config.
