## Context

The server talks to object storage exclusively through the AWS SDK v2 S3 client in
`apps/server/internal/storage/storage.go`: custom endpoint, static credentials,
path-style addressing, region `us-east-1`, and presigned GET URLs. The operations in
use are `HeadBucket`, `CreateBucket`, `PutObject`, `GetObject`, `HeadObject`,
`DeleteObject`, `ListObjectsV2`, and `PresignGetObject`. There is no versioning,
object-lock, tagging, ACL, or explicit multipart-manager usage, so the compatibility
bar for a replacement backend is low.

## Decisions

- **Backend: SeaweedFS.** Apache-2.0, actively maintained, single-container S3 front
  (`weed server -dir=/data -s3` runs master + volume + filer + S3 in one process),
  supports path-style addressing and presigned URLs, and needs no separate identity
  bootstrap beyond credentials. Alternatives rejected:
  - **Garage** — lower footprint but requires a `garage.toml` plus
    `layout assign/apply` and key/bucket bootstrap, which adds installer steps for a
    one-command product. Omitted by decision.
  - **RustFS** — Apache-2.0 and markets itself as a MinIO drop-in, but v1.0.0 and not
    yet battle-tested.
  - **Ceph RGW / Apache Ozone** — near-complete S3 but far too heavy for single-node
    installs.
  - **Scality S3 Server** — upstream declares it unmaintained.
  - **AIStor** — licence-gated; offline mode denies S3 operations.
  - **Cloud S3 (R2/B2/AWS)** — not self-hostable; wrong for the self-hosted product.
- **No server storage rewrite.** The storage service stays S3-generic; only a
  `STORAGE_REGION` passthrough and lightweight `STORAGE_PROVIDER` validation are added.
- **Bucket bootstrap: explicit one-shot `storage-init`.** A new
  `apps/server/cmd/storage-init` binary wraps the existing `EnsureBucket` for the
  documents and temp buckets and runs as a one-shot compose service gated by
  `depends_on: condition: service_completed_successfully`. This mirrors the existing
  `emergent-migrate` pattern, reuses the same S3 client (path-style, checksum config),
  removes the MinIO `mc` client image entirely, and keeps provisioning observable
  rather than coupling bucket creation to server boot. The server only ensures the
  database-backup bucket today (`domain/scheduler/database_backup_task.go`), so
  boot-time auto-create would turn a provisioning failure into a server crash loop.
- **Health probe: SeaweedFS HTTP status.** Replace the MinIO
  `/minio/health/live` probe with the SeaweedFS status endpoint so the server still
  gates on `service_healthy`.
- **Image pinning.** The SeaweedFS image is pinned by digest in the static compose
  files and the installer template; the same pin string is the single source of truth
  asserted by the CLI pin-sync CI check.
- **Upgrade path for existing installs.** Installs that already hold objects in the
  MinIO volume are migrated by mirroring objects from the old endpoint to the new
  backend (any generic S3 client, e.g. `rclone`/`mc mirror` against the new endpoint)
  before the endpoint is flipped. The old MinIO volume is retained until the cutover
  is verified, so rollback is an endpoint flip.

## Risks / mitigations

- **S3 compatibility gaps (presign, listing).** Mitigation: compatibility smoke tests
  against the pinned SeaweedFS image covering upload, download, presigned GET,
  HeadObject, DeleteObject, and ListObjectsV2.
- **Existing-install data migration.** Mitigation: mirror step in the upgrade path,
  retained MinIO volume, documented endpoint-flip rollback; verify object counts and
  spot SHA-256 checksums before deleting the old volume.
- **Checksum/trailer behavior over plain HTTP.** The client already sets
  `RequestChecksumCalculationWhenRequired`; verify streamed uploads (backup ZIP,
  pg_dump) succeed against SeaweedFS.
- **SeaweedFS credentials/bootstrap.** Confirm explicit S3 credentials can be set so
  the installer's generated secret is authoritative, rather than anonymous defaults.

## Non-goals

- Hosted production and dev environments — covered by the infra-repo change
  `prod-object-storage-migration`.
- Multi-node / erasure-coded storage topologies.
- Garage support.
- Any change to the public API, database schema, or storage key format.
