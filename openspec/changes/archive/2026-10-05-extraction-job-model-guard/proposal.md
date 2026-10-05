## Why

The gateway refuses to start extraction when a project has no resolvable generative model (PR #1522), but that gate is gateway-only. The server's extraction-job creation paths still insert a pending job row and only fail **at run time**, when `pkg/adk` cannot resolve a model. A first attempt gated only the admin handler (`POST /api/admin/extraction-jobs`), which left two unguarded creation paths: MCP `queue-reextraction` and project document auto-extract (raw-content create and post-parse). Any non-gateway caller can therefore enqueue a doomed job whose failure surfaces later in extraction history.

## What Changes

- **Gate at the job-creation chokepoint**: the generative-model guard lives in `ObjectExtractionJobsService.CreateJob`, the single entry point used by every creation path. When no effective generative model resolves for the project, it returns the sentinel `ErrNoGenerativeModel` **before any row is inserted**.
- **All creation paths covered**: the admin API, MCP `queue-reextraction`, and document auto-extract all flow through the chokepoint, so none can bypass the gate.
- **Optional resolver seam**: `CreateJob` is gated only when a `GenerativeModelResolver` is attached (`WithGenerativeModelResolver`); a nil resolver disables the gate (fail-open), so existing 3-arg constructor callers and tests keep working.
- **Caller-appropriate surfaces**: the admin API maps the sentinel to **HTTP 409** (`no_generative_model`) with an actionable message, mirroring the gateway's conflict response; MCP `queue-reextraction` returns a clear tool error (not a panic/500); document auto-extract logs and skips the job while the upload/parse still succeeds — it never 500s the document write.
- **Fail-open on resolution errors**: a transient model-config lookup error does not block extraction, matching the gateway's `extractionModelMissing` convention; only a definitive "no model resolves" (empty model) is rejected.
- **Dependency injection via fx**: `modelconfig.Service` is attached to the shared jobs service through the existing modelconfig module; no new import cycle and no migration.
- Run-time behavior is otherwise unchanged: the worker still resolves the model as before, and existing jobs/messages are untouched.

## Capabilities

### New Capabilities

- `extraction-job-model-guard`: every extraction-job creation path (admin API, MCP `queue-reextraction`, document auto-extract) refuses to enqueue a job for a project with no resolvable generative model, before a row is written, and each caller surfaces the refusal appropriately.

### Modified Capabilities

<!-- None: no existing spec covered extraction-job creation. -->

## Impact

- **Backend**: `apps/server/domain/extraction/object_extraction_jobs.go` — `GenerativeModelResolver` seam, `ErrNoGenerativeModel`, `WithGenerativeModelResolver`, chokepoint gate in `CreateJob`; `admin_handler.go` — maps the sentinel to `409 no_generative_model`; `module.go` — attach `*modelconfig.Service` to the shared jobs service.
- **Callers (behavior)**: MCP `queue_reextraction` returns a tool error; documents upload/parse auto-extract logs and skips (upload still succeeds). Admin API may return `409 no_generative_model` instead of `201`.
- **Tests**: DB-backed `apps/server/domain/extraction/object_extraction_model_guard_test.go` (chokepoint + MCP + auto-extract reject/succeed, fail-open) and `documents_autoextract_guard_test.go` (upload survives without a model); admin guard tests retained through the chokepoint.
- **Database**: none.
