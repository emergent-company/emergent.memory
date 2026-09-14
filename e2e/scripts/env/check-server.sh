#!/bin/sh
# check-server.sh — healthcheck for a Memory server environment.
# Usage: check-server.sh [server-url]
# Returns 0 if the server responds to /health with status "healthy".

SERVER_URL="${1}"
if [ -z "$SERVER_URL" ]; then
  echo "  ERROR: server URL not provided and MEMORY_TEST_SERVER not set"
  exit 1
fi

echo "  checking $SERVER_URL ..."

HEALTH=$(curl -sf --max-time 5 "$SERVER_URL/health" 2>/dev/null)
CURL_EXIT=$?

if [ $CURL_EXIT -ne 0 ]; then
  echo "  FAIL: server unreachable at $SERVER_URL (curl exit $CURL_EXIT)"
  exit 1
fi

echo "$HEALTH" | grep -q '"status":"healthy"'
if [ $? -ne 0 ]; then
  echo "  FAIL: server at $SERVER_URL did not report healthy status"
  echo "  response: $HEALTH"
  exit 1
fi

echo "  OK: server healthy"
exit 0
