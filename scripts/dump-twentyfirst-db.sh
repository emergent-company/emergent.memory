#!/bin/bash
set -euo pipefail

# =============================================================================
# dump-twentyfirst-db.sh
#
# Dumps the twentyfirst-io Cloud SQL PostgreSQL database to local CSV files
# that can be consumed by the seed-twentyfirst-db Go seeder.
#
# Requirements:
#   - gcloud CLI installed and authenticated (run: gcloud auth login)
#   - pg_dump and psql available
#
# Output:
#   /tmp/twentyfirst_dump/
#     twentyfirst-db.dump      pg_dump custom format (for schema reference)
#     schema.txt               \dt listing of all tables
#     <table>.csv.gz           one file per exported table
# =============================================================================

GCP_PROJECT="twentyfirst-io"
INSTANCE="dep-database-dev-b3db59nu"
INSTANCE_CONNECTION_NAME="${GCP_PROJECT}:us-central1:${INSTANCE}"
PROXY_VERSION="2.14.1"
PROXY_BIN="/tmp/cloud-sql-proxy"
PROXY_PORT="5433"
DB_NAME="${DB_NAME:-postgres}"
DB_USER="${DB_USER:-postgres}"
DUMP_DIR="${DUMP_DIR:-/root/data/company-catalog}"
PROXY_PID=""

# Tables to export as CSV (space-separated). Override via env or leave empty to
# export ALL tables discovered at runtime.
EXPORT_TABLES="${EXPORT_TABLES:-}"

# -----------------------------------------------------------------------------
cleanup() {
  if [ -n "$PROXY_PID" ] && kill -0 "$PROXY_PID" 2>/dev/null; then
    echo "Stopping Cloud SQL Auth Proxy (pid $PROXY_PID)..."
    kill "$PROXY_PID" 2>/dev/null || true
    wait "$PROXY_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# -----------------------------------------------------------------------------
echo "=== twentyfirst-db dump script ==="
echo "Project:  $GCP_PROJECT"
echo "Instance: $INSTANCE"
echo "Database: $DB_NAME"
echo "User:     $DB_USER"
echo "Output:   $DUMP_DIR"
echo ""

# 1. Check gcloud auth
echo "[1/6] Checking gcloud authentication..."
ACTIVE_ACCOUNT=$(gcloud auth list --filter=status:ACTIVE --format="value(account)" 2>/dev/null | head -1)
if [ -z "$ACTIVE_ACCOUNT" ]; then
  echo ""
  echo "ERROR: No active gcloud account found."
  echo "Please authenticate first:"
  echo ""
  echo "  gcloud auth login"
  echo "  gcloud config set project $GCP_PROJECT"
  echo ""
  exit 1
fi
echo "Authenticated as: $ACTIVE_ACCOUNT"

# 2. Download Cloud SQL Auth Proxy if not cached
echo ""
echo "[2/6] Checking Cloud SQL Auth Proxy..."
if [ ! -f "$PROXY_BIN" ]; then
  echo "Downloading cloud-sql-proxy v${PROXY_VERSION}..."
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64)  PROXY_ARCH="amd64" ;;
    aarch64) PROXY_ARCH="arm64" ;;
    *)       echo "ERROR: Unsupported architecture: $ARCH"; exit 1 ;;
  esac
  PROXY_URL="https://storage.googleapis.com/cloud-sql-connectors/cloud-sql-proxy/v${PROXY_VERSION}/cloud-sql-proxy.linux.${PROXY_ARCH}"
  curl -sSL -o "$PROXY_BIN" "$PROXY_URL"
  chmod +x "$PROXY_BIN"
  echo "Downloaded to $PROXY_BIN"
else
  echo "Using cached proxy at $PROXY_BIN"
fi

# 3. Start Auth Proxy
echo ""
echo "[3/6] Starting Cloud SQL Auth Proxy on localhost:${PROXY_PORT}..."
"$PROXY_BIN" "${INSTANCE_CONNECTION_NAME}" --port "${PROXY_PORT}" &
PROXY_PID=$!
echo "Proxy PID: $PROXY_PID"

# Wait for proxy to be ready
echo "Waiting for proxy to be ready..."
for i in $(seq 1 20); do
  if pg_isready -h 127.0.0.1 -p "$PROXY_PORT" -U "$DB_USER" -q 2>/dev/null; then
    echo "Proxy is ready."
    break
  fi
  if [ "$i" -eq 20 ]; then
    echo "ERROR: Proxy did not become ready after 20s. Check your gcloud auth and instance name."
    exit 1
  fi
  sleep 1
done

export PGPASSWORD="${DB_PASSWORD:-}"
PSQL_CONN="-h 127.0.0.1 -p $PROXY_PORT -U $DB_USER -d $DB_NAME"
PGDUMP_CONN="-h 127.0.0.1 -p $PROXY_PORT -U $DB_USER"

# 4. Create output directory
mkdir -p "$DUMP_DIR"

# 5. Schema introspection — print all tables
echo ""
echo "[4/6] Introspecting database schema..."
echo ""
echo "=== SCHEMAS ==="
psql $PSQL_CONN -c "\dn" 2>/dev/null || true

echo ""
echo "=== ALL TABLES ==="
SCHEMA_OUTPUT=$(psql $PSQL_CONN -c "\dt *.*" 2>/dev/null)
echo "$SCHEMA_OUTPUT"
echo "$SCHEMA_OUTPUT" > "$DUMP_DIR/schema.txt"
echo ""
echo "Schema written to $DUMP_DIR/schema.txt"

# Also get column info for each table
echo ""
echo "=== TABLE COLUMNS ==="
psql $PSQL_CONN -c "
SELECT
  table_schema,
  table_name,
  column_name,
  data_type,
  is_nullable
FROM information_schema.columns
WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY table_schema, table_name, ordinal_position;
" 2>/dev/null | tee "$DUMP_DIR/columns.txt"
echo "Column info written to $DUMP_DIR/columns.txt"

# Row counts per table
echo ""
echo "=== ROW COUNTS ==="
psql $PSQL_CONN -t -c "
SELECT
  'SELECT ' || quote_literal(table_schema || '.' || table_name) || ' AS tbl, COUNT(*) FROM ' || quote_ident(table_schema) || '.' || quote_ident(table_name)
FROM information_schema.tables
WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
  AND table_type = 'BASE TABLE'
ORDER BY table_schema, table_name;
" 2>/dev/null | grep -v '^$' | while read sql; do
  eval psql $PSQL_CONN -t -c "$sql" 2>/dev/null || true
done | tee "$DUMP_DIR/rowcounts.txt"
echo "Row counts written to $DUMP_DIR/rowcounts.txt"

# 6. pg_dump (custom format for schema reference)
echo ""
echo "[5/6] Running pg_dump (custom format)..."
pg_dump $PGDUMP_CONN "$DB_NAME" --format=custom --no-acl --no-owner \
  -f "$DUMP_DIR/twentyfirst-db.dump"
echo "Dump written to $DUMP_DIR/twentyfirst-db.dump"

# 7. Export tables as CSV
echo ""
echo "[6/6] Exporting tables as CSV..."

# If EXPORT_TABLES not set, discover all non-system tables
if [ -z "$EXPORT_TABLES" ]; then
  EXPORT_TABLES=$(psql $PSQL_CONN -t -c "
    SELECT table_schema || '.' || table_name
    FROM information_schema.tables
    WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
      AND table_type = 'BASE TABLE'
    ORDER BY table_schema, table_name;
  " 2>/dev/null | tr -d ' ' | grep -v '^$')
fi

for TABLE in $EXPORT_TABLES; do
  SAFE_NAME=$(echo "$TABLE" | tr '.' '_')
  OUT="$DUMP_DIR/${SAFE_NAME}.csv.gz"
  echo "  Exporting $TABLE -> ${SAFE_NAME}.csv.gz ..."
  psql $PSQL_CONN -c "\COPY $TABLE TO STDOUT CSV HEADER" 2>/dev/null | gzip > "$OUT"
done

echo ""
echo "=== DONE ==="
echo "Files written to $DUMP_DIR:"
ls -lh "$DUMP_DIR"
echo ""
echo "Next step: review schema.txt and columns.txt to design the object mapping,"
echo "then run: go run scripts/seed-twentyfirst-db/main.go"
