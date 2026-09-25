#!/usr/bin/env bash
#
# migrate-object-store.sh — mirror objects from an existing MinIO endpoint to
# the new SeaweedFS (or any S3-compatible) endpoint using rclone, then verify.
#
# Run this BEFORE flipping the server's STORAGE_ENDPOINT, while the old MinIO
# instance is still up. The installer's SeaweedFS host port defaults to 19000 —
# the same port old MinIO used — so bring SeaweedFS up on a distinct temporary
# host port (e.g. 19001) for the mirror; see UPGRADING_OBJECT_STORE.md. The old
# MinIO volume is retained until the cutover is verified, so rollback is an
# endpoint flip.
#
# Requirements: rclone (https://rclone.org/downloads/) on PATH.
#
# Required environment:
#   SRC_S3_ENDPOINT       old MinIO endpoint, e.g. http://localhost:19000
#   SRC_S3_ACCESS_KEY     old MinIO access key
#   SRC_S3_SECRET_KEY     old MinIO secret key
#   DST_S3_ENDPOINT       new SeaweedFS endpoint, e.g. http://localhost:19001
#                         (temporary migration port; the installer takes 19000 later)
#   DST_S3_ACCESS_KEY     new object-store access key
#   DST_S3_SECRET_KEY     new object-store secret key
#
# Optional environment:
#   BUCKETS               space-separated buckets (default: "documents document-temp")
#   CHECK_SPOT            number of objects per bucket to SHA-256 spot-check (default: 3)
#
set -euo pipefail

: "${SRC_S3_ENDPOINT:?set SRC_S3_ENDPOINT (old MinIO endpoint)}"
: "${SRC_S3_ACCESS_KEY:?set SRC_S3_ACCESS_KEY}"
: "${SRC_S3_SECRET_KEY:?set SRC_S3_SECRET_KEY}"
: "${DST_S3_ENDPOINT:?set DST_S3_ENDPOINT (new SeaweedFS endpoint)}"
: "${DST_S3_ACCESS_KEY:?set DST_S3_ACCESS_KEY}"
: "${DST_S3_SECRET_KEY:?set DST_S3_SECRET_KEY}"

BUCKETS="${BUCKETS:-documents document-temp}"
CHECK_SPOT="${CHECK_SPOT:-3}"

if ! command -v rclone >/dev/null 2>&1; then
    echo "ERROR: rclone is required but not on PATH. See https://rclone.org/downloads/" >&2
    exit 1
fi

CONF="$(mktemp)"
chmod 600 "$CONF"
trap 'rm -f "$CONF"' EXIT

# Credentials are written to a private temp config (mode 0600) rather than
# passed on the command line, so they do not leak via `ps`.
cat >"$CONF" <<EOF
[olds3]
type = s3
provider = Other
endpoint = ${SRC_S3_ENDPOINT}
access_key_id = ${SRC_S3_ACCESS_KEY}
secret_access_key = ${SRC_S3_SECRET_KEY}

[news3]
type = s3
provider = Other
endpoint = ${DST_S3_ENDPOINT}
access_key_id = ${DST_S3_ACCESS_KEY}
secret_access_key = ${DST_S3_SECRET_KEY}
EOF

echo "==> Mirroring buckets: ${BUCKETS}"
echo "    from ${SRC_S3_ENDPOINT} -> ${DST_S3_ENDPOINT}"

for bucket in $BUCKETS; do
    echo ""
    echo "==> [${bucket}] create destination bucket (if missing)"
    rclone --config "$CONF" mkdir "news3:${bucket}" 2>/dev/null || true

    echo "==> [${bucket}] copy objects"
    rclone --config "$CONF" copy "olds3:${bucket}" "news3:${bucket}" --transfers 8 --checkers 16

    echo "==> [${bucket}] verify counts"
    src_size="$(rclone --config "$CONF" size --json "olds3:${bucket}")"
    dst_size="$(rclone --config "$CONF" size --json "news3:${bucket}")"
    echo "    source:      ${src_size}"
    echo "    destination: ${dst_size}"

    # Size/count sanity check. `rclone check --checksum` false-fails when the
    # two backends expose no common hash (multipart ETags vs single-part) or
    # disagree on the hash algorithm, so use --size-only here and rely on the
    # SHA-256 spot-check below for content integrity.
    if ! rclone --config "$CONF" check "olds3:${bucket}" "news3:${bucket}" --size-only --one-way; then
        echo "ERROR: size/count verification failed for bucket ${bucket}; keeping the old volume" >&2
        exit 1
    fi

    echo "==> [${bucket}] SHA-256 spot-check (${CHECK_SPOT} objects)"
    checked=0
    while IFS= read -r key; do
        [ -z "$key" ] && continue
        src_hash="$(rclone --config "$CONF" cat "olds3:${bucket}/${key}" | sha256sum | awk '{print $1}')"
        dst_hash="$(rclone --config "$CONF" cat "news3:${bucket}/${key}" | sha256sum | awk '{print $1}')"
        if [ "$src_hash" != "$dst_hash" ]; then
            echo "ERROR: SHA-256 mismatch for ${bucket}/${key} (src=${src_hash} dst=${dst_hash})" >&2
            exit 1
        fi
        echo "    OK ${bucket}/${key} ${src_hash}"
        checked=$((checked + 1))
        [ "$checked" -ge "$CHECK_SPOT" ] && break
    done < <(rclone --config "$CONF" ls "news3:${bucket}" 2>/dev/null | awk '{print $NF}')
done

echo ""
echo "==> Mirror complete. All buckets verified."
echo "    Next: flip the server's STORAGE_ENDPOINT to the new backend."
echo "    For the self-hosted compose that is http://seaweedfs:8333 (STORAGE_PROVIDER=seaweedfs);"
echo "    restart via 'docker compose up -d'. Keep the old MinIO volume until the cutover has"
echo "    soaked; see UPGRADING_OBJECT_STORE.md."
