## ADDED Requirements

### Requirement: Extraction job creation is gated on a resolvable generative model

The server SHALL resolve the addressed project's effective generative model at the extraction-job creation chokepoint (`ObjectExtractionJobsService.CreateJob`, used by every creation path) before inserting or enqueueing a job, using the same resolution signal as the gateway extraction gate (`modelconfig.Service.ResolveGenerativeModel` / effective model config). When no generative model resolves (an empty effective model), creation SHALL be refused and no `kb.object_extraction_jobs` row SHALL be inserted.

The gate SHALL cover every creation path — the admin API (`POST /api/admin/extraction-jobs`), MCP `queue-reextraction`, and project document auto-extract (raw-content create and post-parse). Callers SHALL surface the refusal on their own surface without turning it into an internal error:

- the admin API SHALL respond `409 no_generative_model`;
- the MCP `queue-reextraction` tool SHALL return a clear tool error (not a panic or 500) and insert no row;
- document auto-extract SHALL log and skip creating the job while the document upload still succeeds — it SHALL NOT fail the upload.

The gate SHALL fail open only on an error returned by `ResolveGenerativeModel` to the chokepoint: a transient model-config lookup failure, or a service constructed without a resolver, SHALL NOT block job creation. By contrast, an empty effective model — including one produced when the in-service provider-credential fallback yields nothing (its own resolution error is swallowed inside `ResolveGenerativeModel`, so it surfaces as empty rather than as an error) — is a definitive "no model" and SHALL be refused. The admin guard is project-scoped and SHALL run after project membership enforcement, so an unauthorized project request is refused before model resolution is consulted.

#### Scenario: Admin API rejects with no generative model

- **WHEN** a project has no resolvable generative model and a caller posts a valid create request to the admin API
- **THEN** the server responds `409` with a `no_generative_model` error explaining that no generative model is configured
- **AND** no `kb.object_extraction_jobs` row is inserted for that project

#### Scenario: MCP queue-reextraction rejects with no generative model

- **WHEN** a project has no resolvable generative model and an MCP caller invokes `queue-reextraction`
- **THEN** the tool returns a clear error explaining that no generative model is configured
- **AND** no `kb.object_extraction_jobs` row is inserted

#### Scenario: Document auto-extract skips without failing the upload

- **WHEN** a document is created with auto-extract requested (or parsed with `auto_extract` metadata) and the project has no resolvable generative model
- **THEN** the document creation/parsing still succeeds
- **AND** the extraction trigger is logged and skipped
- **AND** no `kb.object_extraction_jobs` row is inserted

#### Scenario: Resolvable generative model creates the job

- **WHEN** a project's effective generative model resolves
- **THEN** the admin API, MCP `queue-reextraction`, and document auto-extract paths all create the pending extraction job as before

#### Scenario: Resolution error fails open

- **WHEN** `ResolveGenerativeModel` returns an error to the chokepoint (e.g. the project-config lookup fails) rather than an empty model name, or the service has no resolver attached
- **THEN** the server proceeds to create the job, preserving pre-existing behavior

#### Scenario: Provider-fallback failure is treated as no model

- **WHEN** the provider-credential fallback inside `ResolveGenerativeModel` yields no model (its error is swallowed) so the effective model is empty
- **THEN** creation is refused — the empty model, not a swallowed provider error, is what the gate observes

#### Scenario: Membership is enforced before the guard

- **WHEN** an unauthorized caller posts a create request to the admin API for a project they cannot access
- **THEN** the server responds with the authorization error (`403`) without consulting generative-model resolution
