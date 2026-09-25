## Why

MinIO has archived its community edition (`minio/minio` is read-only, AGPL-3.0) and
privatised its community container images: anonymous pulls from `quay.io/minio/*` now
return `401`, the Docker Hub repositories are gone, and `dl.min.io` release binaries
return `410`. Upstream has pivoted to the commercial **AIStor** product, whose images
refuse S3 operations without a licence.

The product ships MinIO as its stock self-hosted object store, so every fresh install
depends on a dead image and an unmaintained server with no future security patches.
The server's storage layer is already generic S3 (`aws-sdk-go-v2`, custom endpoint,
path-style, presigned GET), so the backend can be replaced without reworking
application code.

## What Changes

- Replace the stock self-hosted object store (MinIO) with **SeaweedFS**
  (`chrislusf/seaweedfs`, Apache-2.0, actively maintained), pinned by digest, across
  both self-hosted compose files, the online and offline installers, the CLI installer
  template, the e2e stack, and the docs.
- Keep the server storage layer as-is: S3 endpoint + static credentials, path-style
  addressing, presigned URLs. Add a `STORAGE_REGION` passthrough and provider
  validation only (no behavioral rework, no MinIO-specific calls exist).
- Replace the MinIO `mc` bucket-init container with a deterministic, backend-agnostic
  one-shot init implemented as a new `storage-init` binary in the server image (same
  pattern as `emergent-migrate`), calling the existing `EnsureBucket`.
- Replace the MinIO `/minio/health/live` probe with the SeaweedFS HTTP status probe.
- Update the CLI pin-sync CI check, installer/docker tests, and self-hosted docs.
- Add an upgrade path for existing installs that already hold objects in a MinIO
  volume: mirror objects to the new backend, retain the old volume, and keep an
  endpoint-flip rollback.

## Capabilities

### New Capabilities

- `object-storage-backend`: the stock self-hosted deployment provisions a maintained,
  freely pullable S3-compatible object store with a documented image/version,
  credentials, deterministic bucket bootstrap, health probe, region, and a
  backend-agnostic path-style + presign contract.

### Modified Capabilities

- `backup-restore`: file re-upload on restore is described against the configured
  S3-compatible object store rather than MinIO specifically.

## Impact

- `apps/server/cmd/storage-init/` (new) + `deploy/self-hosted/Dockerfile.server`.
- `apps/server/internal/storage/storage.go` — `STORAGE_REGION`, provider validation.
- `deploy/self-hosted/docker-compose.yml`, `docker-compose.local.yml`,
  `install-online.sh`, `install.sh`, `README.md`.
- `apps/cli/internal/installer/templates.go` + `installer_test.go`,
  `apps/cli/internal/installer/docker_test.go`, `.github/workflows/cli.yml` pin-sync.
- `e2e/docker-compose.yml`, `e2e/.env.example`, `.github/workflows/e2e.yml`
  (GHCR login + infrastructure pull preflight from issue #23).
- Spec deltas: `object-storage-backend` (new), `backup-restore` (modified).
- Cross-repo follow-up: `emergent.memory.infra` change `prod-object-storage-migration`
  converts the production/dev composes and the production backup script.
- No public API changes; no database migration; no server storage-layer rewrite.
