# Upgrading an existing install from MinIO to SeaweedFS

MinIO archived its community edition and privatised its container images
(see `openspec/changes/switch-object-storage-backend/`). New installs use
SeaweedFS. Existing installs still hold their objects in the old `minio_data`
volume, so the upgrade is an **object mirror followed by an endpoint flip** —
never a in-place volume conversion.

The old MinIO volume is retained until the new backend is verified, so rollback
is a single endpoint flip.

## Before you start

- Install [rclone](https://rclone.org/downloads/) on the host running the stack.
- Know both credential pairs:
  - **old MinIO**: `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD` from your existing
    `.env.local`
  - **new object store**: `OBJECT_STORE_ACCESS_KEY` / `OBJECT_STORE_SECRET_KEY`
    (defaults to `emergent` / a generated secret)
- The new SeaweedFS service and `storage-init` must have run at least once
  (`docker compose up -d seaweedfs storage-init`).
- Do the mirror while writes are quiesced, or accept a second delta copy for
  in-flight objects.

## 1. Mirror objects

```bash
cd ~/.memory/docker        # or your INSTALL_DIR/docker

SRC_S3_ENDPOINT=http://localhost:19000 \
SRC_S3_ACCESS_KEY=minioadmin \
SRC_S3_SECRET_KEY="$MINIO_ROOT_PASSWORD" \
DST_S3_ENDPOINT=http://localhost:9000 \
DST_S3_ACCESS_KEY=emergent \
DST_S3_SECRET_KEY="$OBJECT_STORE_SECRET_KEY" \
BUCKETS="documents document-temp" \
  bash /path/to/deploy/self-hosted/migrate-object-store.sh
```

The script copies each bucket, compares object counts, runs an rclone checksum
check, and SHA-256 spot-checks the copied objects. It exits non-zero on any
mismatch and leaves the old volume untouched.

## 2. Verify

```bash
# Destination buckets and object counts (via the S3 API)
rclone lsd news3:                 # with a remote configured for SeaweedFS
rclone size news3:documents

# storage-init succeeded and the server is healthy on the new backend
docker compose logs storage-init
curl -sf http://localhost:3002/health
docker compose logs seaweedfs | tail
```

Only after counts and spot checksums match should you delete the old volume.

## 3. Flip the endpoint

For installs managed by the CLI installer / `install-online.sh`, regenerate the
compose file from the new template (it replaces the `minio`/`minio-init`
services with `seaweedfs`/`storage-init`) and restart:

```bash
docker compose down
docker compose pull
docker compose up -d
```

For hand-managed compose files, replace the `minio` + `minio-init` services with
the `seaweedfs` + `storage-init` services from
`deploy/self-hosted/docker-compose.yml`, point the server at
`STORAGE_ENDPOINT: http://seaweedfs:8333` / `STORAGE_PROVIDER: seaweedfs`, and
keep the old `minio_data` volume mounted (but unused) during the soak period.

## Rollback (endpoint flip back)

The old MinIO container and `minio_data` volume are intentionally retained for a
soak period. To roll back:

1. Re-add the old `minio` service (or `docker compose stop seaweedfs storage-init`).
2. Restore the server's storage env to the MinIO values:
   `STORAGE_PROVIDER: minio`, `STORAGE_ENDPOINT: http://minio:9000`,
   `STORAGE_ACCESS_KEY`/`STORAGE_SECRET_KEY` = the old MinIO root credentials.
3. `docker compose up -d`.
4. Confirm the server health endpoint and a document download.

Because no volume was converted in place, no data restore is required: the old
volume still holds the pre-migration objects.

## Cleanup

After the new backend has soaked and backups of the new volume exist:

```bash
docker compose down
docker volume rm docker_minio_data
```

Keep at least one verified backup of the new `object_store_data` volume before
deleting the old one.
