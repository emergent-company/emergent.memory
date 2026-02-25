#!/bin/bash
set -euo pipefail

# =============================================================================
# seed-twentyfirst-db.sh
#
# Wrapper script to run the standalone twentyfirst-db Go seeder.
# Reads the local CSV dumps and loads them into Emergent graph API.
#
# Set DRY_RUN="true" or SEED_LIMIT="100" to test without doing full run.
# Set RETRY_FAILED="true" to replay relationship batches that failed previously.
# =============================================================================

export SERVER_URL="${SERVER_URL:-http://mcj-emergent.tail0358fa.ts.net:3002}"
export API_KEY="${API_KEY:-emt_e50abca0583305bb5b540afd35fc323007840dd101b91b5cac50bc671ad05a2a}"
export PROJECT_ID="${PROJECT_ID:-dbd87c18-580f-4e42-be64-b5957e1d65ee}"
export DUMP_DIR="${DUMP_DIR:-/root/data/company-catalog}"
export GOWORK="off" # Required to bypass repo go.work file for this standalone module

if [ -z "$API_KEY" ]; then
  echo "ERROR: API_KEY is required."
  exit 1
fi

if [ -z "$PROJECT_ID" ]; then
  echo "ERROR: PROJECT_ID is required."
  exit 1
fi

echo "==============================================="
echo "Starting twentyfirst-db seeder"
echo "Target: $SERVER_URL"
echo "Project: $PROJECT_ID"
echo "Source: $DUMP_DIR"
if [ -n "${DRY_RUN:-}" ]; then
  echo "DRY_RUN: $DRY_RUN"
fi
if [ -n "${SEED_LIMIT:-}" ]; then
  echo "SEED_LIMIT: $SEED_LIMIT"
fi
if [ -n "${RETRY_FAILED:-}" ]; then
  echo "RETRY_FAILED: $RETRY_FAILED"
fi
echo "==============================================="

cd "$(dirname "$0")/seed-twentyfirst-db"
go run .
