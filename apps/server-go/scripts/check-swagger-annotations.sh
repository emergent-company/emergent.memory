#!/bin/bash
#
# check-swagger-annotations.sh - Validate Swagger annotation coverage
#
# Usage:
#   ./scripts/check-swagger-annotations.sh           # Check all handlers
#   ./scripts/check-swagger-annotations.sh --strict  # Exit 1 if coverage < 95%
#
# Returns:
#   0 - Coverage is acceptable
#   1 - Coverage is below threshold (strict mode only)

set -euo pipefail

STRICT_MODE=false
MIN_COVERAGE=95

if [[ "${1:-}" == "--strict" ]]; then
  STRICT_MODE=true
fi

cd "$(dirname "$0")/.."

echo "🔍 Checking Swagger annotation coverage..."
echo

# Find all handler files
HANDLER_FILES=$(find domain -name "handler.go" | sort)
TOTAL_FILES=0
ANNOTATED_FILES=0

MISSING_FILES=()

for file in $HANDLER_FILES; do
  TOTAL_FILES=$((TOTAL_FILES + 1))
  
  # Check if file has at least one @Router annotation (indicates swagger docs)
  if grep -q "^// @Router" "$file"; then
    ANNOTATED_FILES=$((ANNOTATED_FILES + 1))
  else
    MISSING_FILES+=("$file")
  fi
done

COVERAGE=$(awk "BEGIN {printf \"%.1f\", ($ANNOTATED_FILES / $TOTAL_FILES) * 100}")

echo "📊 Coverage Report:"
echo "  Total handler files: $TOTAL_FILES"
echo "  Annotated files:     $ANNOTATED_FILES"
echo "  Coverage:            $COVERAGE%"
echo

if [[ ${#MISSING_FILES[@]} -gt 0 ]]; then
  echo "⚠️  Files missing Swagger annotations:"
  for file in "${MISSING_FILES[@]}"; do
    echo "  - $file"
  done
  echo
fi

if $STRICT_MODE; then
  if (( $(awk "BEGIN {print ($COVERAGE < $MIN_COVERAGE)}") )); then
    echo "❌ FAIL: Coverage $COVERAGE% is below required $MIN_COVERAGE%"
    exit 1
  fi
fi

echo "✅ Annotation coverage check passed"
exit 0
