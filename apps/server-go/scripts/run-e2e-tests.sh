#!/bin/bash
# Run E2E tests with summary statistics
# Usage: ./scripts/run-e2e-tests.sh [options] [test-pattern]
#
# Options:
#   -v, --verbose    Show full test output (default: only show on failure)
#
# Examples:
#   ./scripts/run-e2e-tests.sh              # Run all E2E tests, summary only
#   ./scripts/run-e2e-tests.sh -v           # Run all with verbose output
#   ./scripts/run-e2e-tests.sh GraphSuite   # Run only graph tests
#   ./scripts/run-e2e-tests.sh -v GraphSuite # Verbose + specific suite

set -e

cd "$(dirname "$0")/.."

# Parse arguments
VERBOSE=false
PATTERN=""

while [[ $# -gt 0 ]]; do
    case $1 in
        -v|--verbose)
            VERBOSE=true
            shift
            ;;
        *)
            PATTERN="$1"
            shift
            ;;
    esac
done

RUN_FLAG=""
if [ -n "$PATTERN" ]; then
    RUN_FLAG="-run $PATTERN"
fi

echo "Running E2E tests..."
if [ -n "$PATTERN" ]; then
    echo "Pattern: $PATTERN"
fi
echo "========================================"

# Run tests and capture output
OUTPUT=$(/usr/local/go/bin/go test ./tests/e2e/... -v -count=1 -timeout 15m $RUN_FLAG 2>&1) || true

# Count results
PASSED=$(echo "$OUTPUT" | grep -c -- "--- PASS:" || true)
FAILED=$(echo "$OUTPUT" | grep -c -- "--- FAIL:" || true)
SKIPPED=$(echo "$OUTPUT" | grep -c -- "--- SKIP:" || true)
TOTAL=$((PASSED + FAILED + SKIPPED))

# Extract duration from the final "ok" line
DURATION=$(echo "$OUTPUT" | grep -E "^ok\s+github.com/emergent/emergent-core/tests/e2e" | grep -oE "[0-9]+\.[0-9]+s" || echo "N/A")

# Show output based on verbose flag or failure
if [ "$VERBOSE" = true ]; then
    echo "$OUTPUT"
    echo ""
elif [ "$FAILED" -gt 0 ]; then
    echo ""
    echo "FAILED TESTS:"
    echo "----------------------------------------"
    # Show failed test names and their output
    echo "$OUTPUT" | grep -A 20 -- "--- FAIL:" || true
    echo ""
fi

echo "========================================"
echo "TEST SUMMARY"
echo "========================================"
echo "Passed:   $PASSED"
echo "Failed:   $FAILED"
echo "Skipped:  $SKIPPED"
echo "Total:    $TOTAL"
echo "Duration: $DURATION"
echo ""

if [ "$FAILED" -gt 0 ]; then
    echo "RESULT: FAILED"
    echo ""
    echo "Run with -v for full output"
    exit 1
else
    echo "RESULT: PASSED"
    exit 0
fi
