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
- The mirror needs **both** backends reachable at once, but the installer's
  SeaweedFS host port defaults to `19000` — the same port old MinIO uses. Bring
  SeaweedFS up on a **temporary distinct host port (`19001`)** for the mirror
  (step 1). Do **not** regenerate the compose file / flip the endpoint until the
  mirror and verification are done.
- Do the mirror while writes are quiesced, or accept a second delta copy for
  in-flight objects.

## 1. Start a temporary SeaweedFS on a distinct host port

The old MinIO stack stays up on `19000`. Start the pinned SeaweedFS image on
`19001`, using the same `object_store_data` volume and credentials the new
compose will use (so the mirrored objects remain visible after the flip):

```bash
cd ~/.memory/docker        # or your INSTALL_DIR/docker

# Same digest as the installer template
SWFS=chrislusf/seaweedfs@sha256:ce9e796f1fe6f06968f4c04bdaf8f678dad9c8acdfef3d244133d71bfa6bf882

# Volume the new compose uses (substitute the actual name if your compose
# project differs — check `docker volume ls`)
docker volume create docker_object_store_data

docker run -d --name memory-seaweedfs-migrate \
  -p 19001:8333 \
  -e AWS_ACCESS_KEY_ID=emergent \
  -e AWS_SECRET_ACCESS_KEY="$OBJECT_STORE_SECRET_KEY" \
  -v docker_object_store_data:/data \
  "$SWFS" server -dir=/data -s3
```

## 2. Mirror objects

Mirror from old MinIO (`19000`) to the temporary SeaweedFS (`19001`):

```bash
cd ~/.memory/docker        # or your INSTALL_DIR/docker

SRC_S3_ENDPOINT=http://localhost:19000 \
SRC_S3_ACCESS_KEY=minioadmin \
SRC_S3_SECRET_KEY="$MINIO_ROOT_PASSWORD" \
DST_S3_ENDPOINT=http://localhost:19001 \
DST_S3_ACCESS_KEY=emergent \
DST_S3_SECRET_KEY="$OBJECT_STORE_SECRET_KEY" \
BUCKETS="documents document-temp" \
  bash /path/to/deploy/self-hosted/migrate-object-store.sh
```

The script copies each bucket, compares object counts/sizes, SHA-256
spot-checks the copied objects, and exits non-zero on any mismatch. It leaves
both the old MinIO volume and the new `object_store_data` volume untouched.

## 3. Verify

```bash
# Destination buckets and object counts (via the S3 API)
rclone lsd news3:                 # remote configured for the temporary SeaweedFS on 19001
rclone size news3:documents

# Temporary SeaweedFS logs
docker logs memory-seaweedfs-migrate | tail
```

Only after counts and spot checksums match should you delete the old volume.

## 4. Stop the temporary SeaweedFS

Stop and remove the throwaway container, but keep its volume:

```bash
docker stop memory-seaweedfs-migrate
docker rm memory-seaweedfs-migrate
```

## 5. Flip the endpoint

With the old compose still in place, stop the old stack (this stops MinIO but
keeps the `minio_data` volume):

```bash
docker compose down
```

Then regenerate the compose file from the new template — for installs managed by
the CLI installer / `install-online.sh`, that replaces the `minio`/`minio-init`
services with `seaweedfs`/`storage-init` — and start:

```bash
docker compose pull
docker compose up -d        # SeaweedFS now claims host port 19000 via the template
```

For hand-managed compose files, replace the `minio` + `minio-init` services with
the `seaweedfs` + `storage-init` services from
`deploy/self-hosted/docker-compose.yml`, point the server at
`STORAGE_ENDPOINT: http://seaweedfs:8333` / `STORAGE_PROVIDER: seaweedfs`, and
keep the old `minio_data` volume mounted (but unused) during the soak period.

## Rollback (endpoint flip back)

The old MinIO container and `minio_data` volume are intentionally retained for a
soak period. To roll back:

1. `docker compose stop seaweedfs storage-init` (and stop the new server).
2. Re-add the old `minio` service (or use the old compose file) and restore the
   server's storage env to the MinIO values:
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
