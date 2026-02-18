#!/bin/bash
#
# Workspace Load Test Script
# Tests concurrent workspace creation and warm pool behavior via the API.
#
# Usage: ./test-workspace-load.sh [SERVER_URL] [API_KEY] [CONCURRENCY]
#
# Example:
#   ./test-workspace-load.sh http://localhost:3002 your-api-key 5
#

set -e

# Configuration
SERVER_URL="${1:-http://localhost:3002}"
API_KEY="${2}"
CONCURRENCY="${3:-5}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_section() {
    echo -e "\n${BLUE}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}\n"
}

log_step() { echo -e "${YELLOW}→${NC} $1"; }
log_success() { echo -e "${GREEN}✓${NC} $1"; }
log_error() { echo -e "${RED}✗${NC} $1"; }

if [ -z "$API_KEY" ]; then
    log_error "API_KEY is required"
    echo "Usage: $0 [SERVER_URL] API_KEY [CONCURRENCY]"
    exit 1
fi

workspace_api() {
    local method=$1
    local endpoint=$2
    local data=$3
    if [ -n "$data" ]; then
        curl -s -X "$method" "${SERVER_URL}/api/v1/agent/workspaces${endpoint}" \
            -H "X-API-Key: ${API_KEY}" \
            -H "Content-Type: application/json" \
            -d "$data"
    else
        curl -s -X "$method" "${SERVER_URL}/api/v1/agent/workspaces${endpoint}" \
            -H "X-API-Key: ${API_KEY}"
    fi
}

# Track workspace IDs for cleanup
WORKSPACE_IDS=()
cleanup() {
    log_section "Cleanup: Deleting ${#WORKSPACE_IDS[@]} Workspaces"
    for ws_id in "${WORKSPACE_IDS[@]}"; do
        workspace_api DELETE "/${ws_id}" > /dev/null 2>&1 || true
    done
    log_success "Cleanup complete"
}
trap cleanup EXIT

# ============================================================================
# TEST 1: Sequential baseline — single workspace creation
# ============================================================================
log_section "Test 1: Sequential Baseline (1 workspace)"

BASELINE_START=$(date +%s%N)
BASELINE_RESPONSE=$(workspace_api POST "" '{
    "container_type": "agent_workspace",
    "provider": "gvisor",
    "deployment_mode": "self-hosted"
}')
BASELINE_END=$(date +%s%N)
BASELINE_MS=$(( (BASELINE_END - BASELINE_START) / 1000000 ))

BASELINE_ID=$(echo "$BASELINE_RESPONSE" | jq -r '.id')
if [ -z "$BASELINE_ID" ] || [ "$BASELINE_ID" = "null" ]; then
    log_error "Failed to create baseline workspace"
    echo "$BASELINE_RESPONSE" | jq .
    exit 1
fi
WORKSPACE_IDS+=("$BASELINE_ID")
log_success "Created baseline workspace in ${BASELINE_MS}ms (API response, async provisioning)"

# Wait for it to become ready
log_step "Waiting for baseline workspace to become ready..."
READY_START=$(date +%s%N)
for i in $(seq 1 60); do
    STATUS=$(workspace_api GET "/${BASELINE_ID}" | jq -r '.status')
    if [ "$STATUS" = "ready" ]; then
        READY_END=$(date +%s%N)
        READY_MS=$(( (READY_END - READY_START) / 1000000 ))
        log_success "Baseline workspace ready in ${READY_MS}ms"
        break
    elif [ "$STATUS" = "error" ]; then
        log_error "Baseline workspace errored"
        workspace_api GET "/${BASELINE_ID}" | jq .
        exit 1
    fi
    sleep 1
done

# ============================================================================
# TEST 2: Concurrent creation — N workspaces at once
# ============================================================================
log_section "Test 2: Concurrent Creation (${CONCURRENCY} workspaces)"

PIDS=()
RESULT_FILES=()
CONCURRENT_START=$(date +%s%N)

for i in $(seq 1 "$CONCURRENCY"); do
    RESULT_FILE=$(mktemp)
    RESULT_FILES+=("$RESULT_FILE")
    (
        CREATE_START=$(date +%s%N)
        RESPONSE=$(workspace_api POST "" '{
            "container_type": "agent_workspace",
            "provider": "gvisor",
            "deployment_mode": "self-hosted"
        }')
        CREATE_END=$(date +%s%N)
        CREATE_MS=$(( (CREATE_END - CREATE_START) / 1000000 ))
        WS_ID=$(echo "$RESPONSE" | jq -r '.id')
        echo "${WS_ID}|${CREATE_MS}" > "$RESULT_FILE"
    ) &
    PIDS+=($!)
done

# Wait for all creates to return
for pid in "${PIDS[@]}"; do
    wait "$pid" 2>/dev/null || true
done

CONCURRENT_END=$(date +%s%N)
CONCURRENT_TOTAL_MS=$(( (CONCURRENT_END - CONCURRENT_START) / 1000000 ))

# Collect results
CREATED=0
FAILED=0
CREATE_TIMES=()
for rf in "${RESULT_FILES[@]}"; do
    if [ -f "$rf" ]; then
        RESULT=$(cat "$rf")
        WS_ID=$(echo "$RESULT" | cut -d'|' -f1)
        CREATE_MS=$(echo "$RESULT" | cut -d'|' -f2)
        if [ -n "$WS_ID" ] && [ "$WS_ID" != "null" ]; then
            WORKSPACE_IDS+=("$WS_ID")
            CREATE_TIMES+=("$CREATE_MS")
            CREATED=$((CREATED + 1))
        else
            FAILED=$((FAILED + 1))
        fi
        rm -f "$rf"
    fi
done

log_success "Created ${CREATED}/${CONCURRENCY} workspaces, ${FAILED} failed"
log_step "Total concurrent creation time: ${CONCURRENT_TOTAL_MS}ms"

if [ ${#CREATE_TIMES[@]} -gt 0 ]; then
    MIN_TIME=${CREATE_TIMES[0]}
    MAX_TIME=${CREATE_TIMES[0]}
    SUM=0
    for t in "${CREATE_TIMES[@]}"; do
        SUM=$((SUM + t))
        [ "$t" -lt "$MIN_TIME" ] && MIN_TIME=$t
        [ "$t" -gt "$MAX_TIME" ] && MAX_TIME=$t
    done
    AVG=$((SUM / ${#CREATE_TIMES[@]}))
    echo "  Min API response: ${MIN_TIME}ms"
    echo "  Max API response: ${MAX_TIME}ms"
    echo "  Avg API response: ${AVG}ms"
fi

# ============================================================================
# TEST 3: Wait for all to become ready
# ============================================================================
log_section "Test 3: Provisioning Complete"

PROV_START=$(date +%s%N)
READY_COUNT=0
ERROR_COUNT=0

# Only check the concurrently created workspaces (skip baseline)
CHECK_IDS=("${WORKSPACE_IDS[@]:1}")

for ws_id in "${CHECK_IDS[@]}"; do
    (
        for i in $(seq 1 120); do
            STATUS=$(workspace_api GET "/${ws_id}" | jq -r '.status' 2>/dev/null)
            if [ "$STATUS" = "ready" ]; then
                echo "ready" > "/tmp/ws-status-${ws_id}"
                exit 0
            elif [ "$STATUS" = "error" ]; then
                echo "error" > "/tmp/ws-status-${ws_id}"
                exit 1
            fi
            sleep 1
        done
        echo "timeout" > "/tmp/ws-status-${ws_id}"
    ) &
done
wait

PROV_END=$(date +%s%N)
PROV_MS=$(( (PROV_END - PROV_START) / 1000000 ))

for ws_id in "${CHECK_IDS[@]}"; do
    STATUS_FILE="/tmp/ws-status-${ws_id}"
    if [ -f "$STATUS_FILE" ]; then
        STATUS=$(cat "$STATUS_FILE")
        rm -f "$STATUS_FILE"
        if [ "$STATUS" = "ready" ]; then
            READY_COUNT=$((READY_COUNT + 1))
        else
            ERROR_COUNT=$((ERROR_COUNT + 1))
        fi
    else
        ERROR_COUNT=$((ERROR_COUNT + 1))
    fi
done

log_success "${READY_COUNT}/${#CHECK_IDS[@]} workspaces became ready in ${PROV_MS}ms"
if [ "$ERROR_COUNT" -gt 0 ]; then
    log_error "${ERROR_COUNT} workspaces failed to provision"
fi

# ============================================================================
# TEST 4: Verify all ready workspaces are functional
# ============================================================================
log_section "Test 4: Functionality Verification"

FUNCTIONAL=0
for ws_id in "${CHECK_IDS[@]}"; do
    STATUS=$(workspace_api GET "/${ws_id}" | jq -r '.status')
    if [ "$STATUS" = "ready" ]; then
        EXEC_RESULT=$(workspace_api POST "/${ws_id}/bash" '{"command": "echo ok && echo $EMERGENT_API_URL"}')
        EXIT_CODE=$(echo "$EXEC_RESULT" | jq -r '.exit_code')
        STDOUT=$(echo "$EXEC_RESULT" | jq -r '.stdout')
        if [ "$EXIT_CODE" = "0" ] && echo "$STDOUT" | grep -q "ok"; then
            FUNCTIONAL=$((FUNCTIONAL + 1))
        fi
    fi
done

log_success "${FUNCTIONAL}/${READY_COUNT} workspaces are functional"

# ============================================================================
# Summary
# ============================================================================
log_section "Load Test Summary"

echo "  Server:                  $SERVER_URL"
echo "  Concurrency:             $CONCURRENCY"
echo "  ---"
echo "  Baseline (single):       ${BASELINE_MS}ms API / ${READY_MS:-?}ms total"
echo "  Concurrent creates:      ${CREATED}/${CONCURRENCY} succeeded"
echo "  Concurrent API time:     ${CONCURRENT_TOTAL_MS}ms"
echo "  Provisioning time:       ${PROV_MS}ms"
echo "  Ready:                   ${READY_COUNT}/${CREATED}"
echo "  Functional:              ${FUNCTIONAL}/${READY_COUNT}"

# Success criteria
SUCCESS_RATE=$(echo "scale=2; $READY_COUNT / $CONCURRENCY * 100" | bc 2>/dev/null || echo "0")
echo ""
if [ "$READY_COUNT" -ge "$((CONCURRENCY * 80 / 100))" ]; then
    log_success "Load test PASSED (${SUCCESS_RATE}% success rate)"
else
    log_error "Load test FAILED (${SUCCESS_RATE}% success rate, need >= 80%)"
    exit 1
fi
