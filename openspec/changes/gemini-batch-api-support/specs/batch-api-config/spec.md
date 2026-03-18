## ADDED Requirements

### Requirement: Org and project provider configs expose batch API settings
`OrgProviderConfig` and `ProjectProviderConfig` SHALL each include three new fields:
- `use_batch_api` (bool, default `false`) — enables the batch path for background workers
- `batch_gcs_bucket` (string, default `""`) — GCS bucket name used for batch I/O
- `batch_flush_interval_seconds` (int, default `300`) — seconds between submit-worker flush cycles

These fields SHALL be stored in `kb.org_provider_configs` and `kb.project_provider_configs` via a Goose migration.

#### Scenario: Default values on new config
- **WHEN** a new provider config is created without specifying batch fields
- **THEN** `use_batch_api` is `false`, `batch_gcs_bucket` is `""`, and `batch_flush_interval_seconds` is `300`

#### Scenario: Batch fields are persisted on upsert
- **WHEN** an org or project provider config is upserted with `use_batch_api=true`, a bucket name, and a custom flush interval
- **THEN** those values are stored and returned in subsequent GET responses

### Requirement: Enabling batch API requires a GCS bucket when provider is Vertex AI
The system SHALL validate on config save that if `use_batch_api=true` and the provider is `google-vertex`, then `batch_gcs_bucket` MUST be non-empty. The system SHALL return a `400 Bad Request` error if this constraint is violated.

#### Scenario: Attempt to enable batch without bucket
- **WHEN** a user sets `use_batch_api=true` with an empty `batch_gcs_bucket` for a Vertex AI provider
- **THEN** the API returns `400 Bad Request` with a message indicating the bucket is required

#### Scenario: Batch with Google AI provider is silently ignored
- **WHEN** `use_batch_api=true` is set for a `google` (API key) provider
- **THEN** the config is saved without error and background workers fall back to real-time calls (batch is not supported for Google AI)

### Requirement: Batch config is exposed in credential resolution
`ResolvedCredential` (in `pkg/adk/credentials.go`) and `ResolvedEmbeddingCredential` (in `pkg/embeddings/credentials.go`) SHALL each include `UseBatchAPI bool`, `BatchGCSBucket string`, and `BatchFlushIntervalSeconds int` fields, populated by the credential resolution hierarchy (project -> org -> defaults).

#### Scenario: Project config overrides org config for batch settings
- **WHEN** an org has `use_batch_api=false` and a project under that org has `use_batch_api=true` with a bucket set
- **THEN** resolving credentials for that project returns `UseBatchAPI=true` and the project's bucket

#### Scenario: Org config applies when project has no override
- **WHEN** an org has `use_batch_api=true` and a bucket set, and a project has no provider config
- **THEN** resolving credentials for that project returns `UseBatchAPI=true` and the org's bucket

### Requirement: Batch config fields are returned in provider config API responses
The `ProviderConfigResponse` and `ProjectProviderConfigResponse` types SHALL include `use_batch_api`, `batch_gcs_bucket`, and `batch_flush_interval_seconds` so that admin UI and CLI can display and manage these settings.

#### Scenario: Config response includes batch fields
- **WHEN** `GET /orgs/:orgID/provider` or `GET /projects/:projectID/provider` is called
- **THEN** the response body includes `use_batch_api`, `batch_gcs_bucket`, and `batch_flush_interval_seconds`

### Requirement: GCS bucket region must match Vertex AI location
When `use_batch_api=true`, the GCS bucket MUST be in the same region or multi-region as the `location` field of the Vertex AI provider config (e.g. a `us-central1` Vertex AI config requires a bucket in `us-central1` or the `US` multi-region). This is a hard Vertex AI enforcement — a mismatched bucket will cause batch jobs to fail. The system SHALL warn on config save if the detected bucket location does not match the configured Vertex AI location, and SHALL surface a clear error message directing the user to use a co-located bucket.

#### Scenario: Bucket region matches Vertex AI location
- **WHEN** the bucket is in `us-central1` and the Vertex AI location is `us-central1`
- **THEN** the config is saved without a region warning

#### Scenario: Bucket region does not match Vertex AI location
- **WHEN** the bucket is in `europe-west4` and the Vertex AI location is `us-central1`
- **THEN** the API returns a warning or error indicating the region mismatch and directing the user to create a bucket in `us-central1` or the `US` multi-region

### Requirement: GCS bucket accessibility is validated on config save using Memory's service account
When `use_batch_api=true` and a bucket is provided for a Vertex AI config, the system SHALL attempt to write and delete a small test object using Memory's configured service account credentials to verify basic bucket reachability. If the test fails, the system SHALL return a `422 Unprocessable Entity` error.

The validation response SHALL include a prominent notice that this test only validates Memory's own access — the Vertex AI Service Agent (`service-{PROJECT_NUMBER}@gcp-sa-aiplatform.iam.gserviceaccount.com`) requires a separate IAM grant on the bucket, which must be done by the user. The API response SHALL link to the setup guide.

#### Scenario: Bucket is reachable by Memory's SA
- **WHEN** Memory's service account can write a test object to the provided GCS bucket
- **THEN** the config is saved successfully with a notice reminding the user to grant the Vertex AI SA access

#### Scenario: Bucket is unreachable by Memory's SA
- **WHEN** Memory's service account cannot write to the provided GCS bucket
- **THEN** the API returns `422 Unprocessable Entity` indicating the bucket access check failed

### Requirement: Setup guide is linked from batch config validation responses
All validation errors and warnings related to `batch_gcs_bucket` (missing bucket, region mismatch, access failure) SHALL include a reference to the user-facing GCS setup guide in the error message or response body.

#### Scenario: Validation error includes setup guide reference
- **WHEN** any batch bucket validation fails
- **THEN** the error response includes a URL or reference to the GCS setup guide
