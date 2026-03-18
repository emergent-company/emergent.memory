## MODIFIED Requirements

### Requirement: Provider config entities include batch API fields
`OrgProviderConfig` and `ProjectProviderConfig` Bun ORM structs SHALL include the following additional fields mapped to database columns:

| Field | Column | Type | Default |
|---|---|---|---|
| `UseBatchAPI` | `use_batch_api` | `bool` | `false` |
| `BatchGCSBucket` | `batch_gcs_bucket` | `text` | `''` |
| `BatchFlushIntervalSeconds` | `batch_flush_interval_seconds` | `int` | `300` |

A Goose migration SHALL add these columns to `kb.org_provider_configs` and `kb.project_provider_configs` with their defaults.

#### Scenario: Existing provider configs are unaffected by migration
- **WHEN** the migration runs on a database with existing provider config rows
- **THEN** those rows have `use_batch_api=false`, `batch_gcs_bucket=''`, and `batch_flush_interval_seconds=300`

#### Scenario: New fields round-trip through upsert and fetch
- **WHEN** an org provider config is upserted with `use_batch_api=true`, `batch_gcs_bucket='my-bucket'`, `batch_flush_interval_seconds=600`
- **THEN** a subsequent fetch of that config returns the same values

### Requirement: ResolvedCredential exposes batch API settings
The `ResolvedCredential` struct in `pkg/adk/credentials.go` SHALL include:
- `UseBatchAPI bool`
- `BatchGCSBucket string`
- `BatchFlushIntervalSeconds int`

The `ResolvedEmbeddingCredential` struct in `pkg/embeddings/credentials.go` SHALL include the same three fields.

`CredentialService.Resolve` (and the `decryptOrgConfig` / `decryptProjectConfig` helpers) SHALL populate these fields from the stored config. When falling back to environment variables, all three SHALL default to `false`, `""`, and `300` respectively.

#### Scenario: Resolved credential reflects project-level batch settings
- **WHEN** a project config has `use_batch_api=true` and `batch_gcs_bucket='proj-bucket'`
- **THEN** `CredentialService.Resolve` returns a `ResolvedCredential` with `UseBatchAPI=true` and `BatchGCSBucket='proj-bucket'`

#### Scenario: Resolved credential defaults when using env-var fallback
- **WHEN** credentials are resolved from environment variables (no DB config)
- **THEN** `UseBatchAPI=false`, `BatchGCSBucket=""`, `BatchFlushIntervalSeconds=300`

### Requirement: LLMUsageEvent records whether the call was batch
`LLMUsageEvent` SHALL include a new `is_batch` boolean column (default `false`) in `kb.llm_usage_events`. A Goose migration SHALL add this column.

The `UsageService.calculateCost` method SHALL apply a `0.5` multiplier to the calculated cost when `IsBatch=true` on the event, reflecting that Vertex AI batch calls are billed at 50% of retail pricing.

#### Scenario: Real-time usage event has is_batch=false
- **WHEN** a `TrackingModel.GenerateContent` call triggers `RecordAsync`
- **THEN** the persisted `LLMUsageEvent` has `is_batch=false` and cost is calculated at full retail price

#### Scenario: Batch usage event has is_batch=true and halved cost
- **WHEN** `BatchPollWorker` creates an `LLMUsageEvent` after processing batch results
- **THEN** the event has `is_batch=true` and `estimated_cost_usd` is 50% of the retail-rate calculation

#### Scenario: Migration preserves existing usage events
- **WHEN** the migration adds `is_batch` to `kb.llm_usage_events`
- **THEN** existing rows have `is_batch=false` (column default)
