#!/bin/bash
# Performance Benchmark Script for Go Server
# Compares key metrics against targets defined in design.md

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_GO_DIR="${SCRIPT_DIR}"
RESULTS_DIR="${SERVER_GO_DIR}/benchmark_results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Targets from design.md
TARGET_COLD_START_MS=2000      # <2s
TARGET_P99_LATENCY_MS=120      # <120ms
TARGET_MEMORY_IDLE_MB=100      # <100MB
TARGET_BINARY_SIZE_MB=50       # <50MB

mkdir -p "$RESULTS_DIR"

echo "========================================"
echo "Go Server Performance Benchmark"
echo "Timestamp: $TIMESTAMP"
echo "========================================"
echo ""

# 1. Binary Size
echo "1. Checking Binary Size..."
cd "$SERVER_GO_DIR"
/usr/local/go/bin/go build -o /tmp/server-go-bench ./cmd/server 2>/dev/null
BINARY_SIZE_BYTES=$(stat -c%s /tmp/server-go-bench 2>/dev/null || stat -f%z /tmp/server-go-bench)
BINARY_SIZE_MB=$(echo "scale=2; $BINARY_SIZE_BYTES / 1048576" | bc)
echo "   Binary size: ${BINARY_SIZE_MB} MB (target: <${TARGET_BINARY_SIZE_MB} MB)"
if (( $(echo "$BINARY_SIZE_MB < $TARGET_BINARY_SIZE_MB" | bc -l) )); then
    echo -e "   ${GREEN}✓ PASS${NC}"
    BINARY_RESULT="PASS"
else
    echo -e "   ${RED}✗ FAIL${NC}"
    BINARY_RESULT="FAIL"
fi
rm -f /tmp/server-go-bench
echo ""

# 2. Build with optimizations for size measurement
echo "2. Building optimized binary..."
/usr/local/go/bin/go build -ldflags="-s -w" -o /tmp/server-go-bench-stripped ./cmd/server 2>/dev/null
STRIPPED_SIZE_BYTES=$(stat -c%s /tmp/server-go-bench-stripped 2>/dev/null || stat -f%z /tmp/server-go-bench-stripped)
STRIPPED_SIZE_MB=$(echo "scale=2; $STRIPPED_SIZE_BYTES / 1048576" | bc)
echo "   Stripped binary: ${STRIPPED_SIZE_MB} MB"
rm -f /tmp/server-go-bench-stripped
echo ""

# 3. Test Count
echo "3. Running E2E test suite..."
TEST_START=$(date +%s%N)
TEST_OUTPUT=$(cd "$SERVER_GO_DIR" && POSTGRES_PASSWORD=emergent-dev-password /usr/local/go/bin/go test ./tests/e2e/... -count=1 2>&1 || true)
TEST_END=$(date +%s%N)
TEST_DURATION_MS=$(( (TEST_END - TEST_START) / 1000000 ))

PASS_COUNT=$(echo "$TEST_OUTPUT" | grep -c "^--- PASS" || echo "0")
FAIL_COUNT=$(echo "$TEST_OUTPUT" | grep -c "^--- FAIL" || echo "0")
echo "   Tests passed: $PASS_COUNT"
echo "   Tests failed: $FAIL_COUNT"
echo "   Test duration: ${TEST_DURATION_MS}ms"
if [ "$FAIL_COUNT" -eq 0 ]; then
    echo -e "   ${GREEN}✓ All tests pass${NC}"
    TEST_RESULT="PASS"
else
    echo -e "   ${RED}✗ Some tests failed${NC}"
    TEST_RESULT="FAIL"
fi
echo ""

# 4. Code Statistics
echo "4. Code Statistics..."
GO_FILES=$(find "$SERVER_GO_DIR" -name "*.go" -not -path "*/vendor/*" | wc -l)
GO_LINES=$(find "$SERVER_GO_DIR" -name "*.go" -not -path "*/vendor/*" -exec cat {} + | wc -l)
echo "   Go files: $GO_FILES"
echo "   Go lines: $GO_LINES"
echo ""

# 5. Module Count
echo "5. Domain Modules..."
DOMAIN_MODULES=$(ls -d "$SERVER_GO_DIR"/domain/*/ 2>/dev/null | wc -l)
echo "   Domain modules: $DOMAIN_MODULES"
echo ""

# Summary
echo "========================================"
echo "BENCHMARK SUMMARY"
echo "========================================"
echo ""
echo "| Metric           | Result       | Target    | Status |"
echo "|------------------|--------------|-----------|--------|"
printf "| Binary Size      | %6.1f MB    | <%3d MB   | %s |\n" "$BINARY_SIZE_MB" "$TARGET_BINARY_SIZE_MB" "$BINARY_RESULT"
printf "| Stripped Size    | %6.1f MB    | -         | -      |\n" "$STRIPPED_SIZE_MB"
printf "| E2E Tests        | %4d passed  | 0 failed  | %s |\n" "$PASS_COUNT" "$TEST_RESULT"
printf "| Go Files         | %4d         | -         | -      |\n" "$GO_FILES"
printf "| Go Lines         | %6d       | -         | -      |\n" "$GO_LINES"
printf "| Domain Modules   | %4d         | -         | -      |\n" "$DOMAIN_MODULES"
echo ""

# Save results to file
RESULTS_FILE="$RESULTS_DIR/benchmark_$TIMESTAMP.json"
cat > "$RESULTS_FILE" << EOF
{
  "timestamp": "$TIMESTAMP",
  "binary_size_mb": $BINARY_SIZE_MB,
  "stripped_binary_size_mb": $STRIPPED_SIZE_MB,
  "binary_target_mb": $TARGET_BINARY_SIZE_MB,
  "binary_result": "$BINARY_RESULT",
  "e2e_tests_passed": $PASS_COUNT,
  "e2e_tests_failed": $FAIL_COUNT,
  "e2e_test_duration_ms": $TEST_DURATION_MS,
  "test_result": "$TEST_RESULT",
  "go_files": $GO_FILES,
  "go_lines": $GO_LINES,
  "domain_modules": $DOMAIN_MODULES,
  "targets": {
    "cold_start_ms": $TARGET_COLD_START_MS,
    "p99_latency_ms": $TARGET_P99_LATENCY_MS,
    "memory_idle_mb": $TARGET_MEMORY_IDLE_MB,
    "binary_size_mb": $TARGET_BINARY_SIZE_MB
  }
}
EOF
echo "Results saved to: $RESULTS_FILE"
echo ""

# Notes about runtime benchmarks
echo "========================================"
echo "NOTES"
echo "========================================"
echo ""
echo "Runtime benchmarks (cold start, P99 latency, memory) require"
echo "the server to be running. To measure these:"
echo ""
echo "1. Start the Go server:"
echo "   cd apps/server-go && POSTGRES_PASSWORD=... go run ./cmd/server"
echo ""
echo "2. Measure cold start:"
echo "   time curl http://localhost:3002/health"
echo ""
echo "3. Measure P99 latency (requires 'hey' or 'wrk'):"
echo "   hey -n 1000 -c 10 http://localhost:3002/health"
echo ""
echo "4. Measure memory (while server is running):"
echo "   ps -o rss= -p \$(pgrep -f 'go run.*cmd/server')"
echo ""
