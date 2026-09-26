## ADDED Requirements

### Requirement: Maintained, freely pullable object store
The stock self-hosted deployment SHALL provision an S3-compatible object store from a maintained, freely pullable image, pinned by digest in the compose files and the CLI installer template. The image SHALL NOT be an end-of-life or registry-locked distribution.

#### Scenario: Fresh host pulls without registry auth
- **WHEN** an operator installs on a clean host with no private registry credentials
- **THEN** every object-store image pull succeeds anonymously
- **THEN** `docker compose up` starts the object store

#### Scenario: Backend is not an EOL dependency
- **WHEN** the pinned object-store image is inspected
- **THEN** it resolves to a maintained upstream project (not the archived MinIO community edition)
- **THEN** the pin is recorded in one source of truth shared by the static compose files and the installer template, and the CLI pin-sync check asserts it

### Requirement: Backend-agnostic S3 client
The server SHALL perform all object storage through its generic S3-compatible client (custom endpoint, static credentials, path-style addressing, configurable region). It SHALL NOT depend on MinIO-specific APIs or MinIO-only endpoints.

#### Scenario: Presigned download works
- **WHEN** a client requests a signed download URL for an uploaded object
- **THEN** the URL is signed against the configured endpoint
- **THEN** a subsequent HTTP GET of that URL returns the object bytes

#### Scenario: Bucket creation works through the S3 API
- **WHEN** the documents bucket does not exist
- **THEN** the deployment creates it through the S3 API
- **THEN** a subsequent `HeadBucket` succeeds

#### Scenario: Region is configurable
- **WHEN** `STORAGE_REGION` is set
- **THEN** the client signs requests with that region

#### Scenario: Unknown provider fails fast
- **WHEN** `STORAGE_PROVIDER` is set to an unsupported value
- **THEN** server startup fails with an actionable error rather than silently degrading

### Requirement: Deterministic bucket bootstrap
The stock deployment SHALL ensure the documents and document-temp buckets exist before the server serves requests, through an explicit one-shot init step that gates server start (`depends_on: condition: service_completed_successfully`). The init SHALL use the same backend-agnostic S3 client as the server.

#### Scenario: Fresh stack provisions buckets
- **WHEN** the stock stack starts against empty storage
- **THEN** the init step creates the documents and document-temp buckets
- **THEN** the server starts only after init completes successfully

#### Scenario: Init failure blocks the server
- **WHEN** the init step cannot reach the object store
- **THEN** init exits non-zero
- **THEN** the server does not start

### Requirement: Object store health signal
The stock deployment SHALL define a health check for the object-store service, and the server SHALL gate its start on that health.

#### Scenario: Unhealthy store blocks the server
- **WHEN** the object-store health probe fails
- **THEN** compose marks the service unhealthy
- **THEN** the server does not start

#### Scenario: Healthy store permits start
- **WHEN** the object-store health probe succeeds
- **THEN** the server proceeds to start

### Requirement: No MinIO tooling in the runtime path
The stock deployment SHALL NOT require the MinIO `mc` client or MinIO server image to provision or run storage.

#### Scenario: No MinIO images referenced
- **WHEN** the stock compose files and the CLI installer template are inspected
- **THEN** no service references a MinIO image
- **THEN** bucket bootstrap uses the server image rather than a MinIO client image
