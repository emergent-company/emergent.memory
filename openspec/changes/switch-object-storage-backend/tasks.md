## 1. Specs & tests first (TDD)

- [ ] 1.1 Land spec deltas: `specs/object-storage-backend/spec.md` (ADDED), `specs/backup-restore/spec.md` (MODIFIED)
- [ ] 1.2 Add a storage-compatibility unit test suite covering upload, download, presigned GET, HeadObject, DeleteObject, and ListObjectsV2 (unit-level with the existing client contract)
- [ ] 1.3 Add `STORAGE_REGION` + `STORAGE_PROVIDER` config tests (defaults, override, invalid value)

## 2. Server — storage config

- [ ] 2.1 `apps/server/internal/storage/storage.go`: pass `STORAGE_REGION` through (already read) and validate `STORAGE_PROVIDER` (accepted backend id; unknown value fails fast with an actionable error)
- [ ] 2.2 Confirm `Region` is applied to signing; keep path-style and `RequestChecksumCalculationWhenRequired` unchanged
- [ ] 2.3 Verify `go test ./apps/server/internal/storage/...`

## 3. Server — `storage-init` binary

- [ ] 3.1 Add `apps/server/cmd/storage-init/main.go` calling `EnsureBucket` for documents + temp buckets
- [ ] 3.2 Add the build + `COPY` to `/usr/local/bin/emergent-storage-init` in `deploy/self-hosted/Dockerfile.server`
- [ ] 3.3 Unit-test bucket selection (documents/temp, env overrides) and exit-code-on-failure behavior

## 4. Installer + static compose files

- [ ] 4.1 Add the object-store image constant (digest-pinned) and `StorageInitImage`/entrypoint to `apps/cli/internal/installer/templates.go`; remove MinIO constants
- [ ] 4.2 Replace the MinIO service + `minio-init` in `deploy/self-hosted/docker-compose.yml` and `docker-compose.local.yml` with the SeaweedFS service + `storage-init` service, wiring `depends_on: condition: service_completed_successfully`
- [ ] 4.3 Mirror the same changes in `install-online.sh` and `install.sh`; add `STORAGE_REGION`
- [ ] 4.4 Update `apps/cli/internal/installer/installer_test.go` and `docker_test.go` expected images/registry assertions
- [ ] 4.5 Update the MinIO pin-sync block in `.github/workflows/cli.yml` to the new object-store pin
- [ ] 4.6 Update `deploy/self-hosted/README.md` (env var table, credentials, ports, healthcheck)

## 5. e2e stack

- [ ] 5.1 Replace MinIO + `minio-init` in `e2e/docker-compose.yml` with SeaweedFS + `storage-init`; update `e2e/.env.example`
- [ ] 5.2 `.github/workflows/e2e.yml`: add GHCR login and a distinct "Preflight: pull stack images" step before "Start server stack" so image-pull failure reads as infrastructure, not failing tests
- [ ] 5.3 Confirm the `api` and `integration` jobs boot the stack on a clean runner

## 6. Upgrade path for existing installs

- [ ] 6.1 Document and script the object mirror from the old MinIO endpoint to the new backend (generic S3 client), run before the endpoint flip
- [ ] 6.2 Verify object counts + spot SHA-256 checksums; retain the old volume until verified
- [ ] 6.3 Document endpoint-flip rollback

## 7. Verify

- [ ] 7.1 `go build ./...` in `apps/server` and `apps/cli`
- [ ] 7.2 `go test ./apps/server/internal/storage/... ./apps/cli/internal/installer/...`
- [ ] 7.3 `task lint` (or the repo's golangci-lint invocation)
- [ ] 7.4 `docker bake`/`build.sh` builds the server image with the new `storage-init` binary
- [ ] 7.5 Anonymous `docker pull` of the pinned object-store image on a clean host
- [ ] 7.6 Fresh stack: buckets exist, server healthy, presigned download works
- [ ] 7.7 No `quay.io/minio` or `minio/minio` references remain (`rg` across the repo)
