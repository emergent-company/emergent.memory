# Go Server Combined Test Coverage Report

**Last Updated:** 2026-01-18

## Summary

| Metric             | Unit Tests | E2E Tests | Combined  |
| ------------------ | ---------- | --------- | --------- |
| Statement Coverage | 17.5%      | 45.2%     | **56.3%** |
| Functions at 100%  | 279        | 173+      | **400**   |

## Coverage Progress

| Date       | Combined Coverage | Functions at 100% | Notes                           |
| ---------- | ----------------- | ----------------- | ------------------------------- |
| Initial    | 54.7%             | 389               | Baseline measurement            |
| 2026-01-18 | 55.9%             | 392               | Added useraccess, invites tests |
| 2026-01-18 | 56.3%             | 400               | Added events tests              |

## Recent Additions

### Events Endpoint Tests (2026-01-18)

- Added `events_test.go` with 6 tests
- New coverage:
  - `events/handler.go:NewHandler` - 100%
  - `events/handler.go:HandleConnectionsCount` - 100%
  - `events/service.go:NewService` - 100%
  - `events/module.go:RegisterRoutesManual` - 100%
  - `events/handler.go:heartbeatLoop` - 83.3%
  - `events/handler.go:HandleStream` - 13.5%

### User Access & Invites Tests (2026-01-18)

- Added `useraccess_test.go` with 8 tests
- Added `invites_test.go` with 9 tests
- New coverage for user access tree and pending invites endpoints

## Uncovered Functions by Package

Top packages with functions at 0% coverage:

| Package           | Functions at 0% |
| ----------------- | --------------- |
| domain/extraction | 46              |
| domain/datasource | 42              |
| domain/email      | 22              |
| domain/events     | 14 (was 20)     |
| pkg/logger        | 14              |
| internal/jobs     | 13              |
| domain/devtools   | 12              |
| pkg/embeddings    | 10              |
| domain/chunks     | 10              |

## Files

- `unit_v3.out` - Unit test coverage profile (17.5%)
- `e2e_v3.out` - E2E test coverage profile (45.2%)
- `combined_v3.out` - Merged coverage profile (56.3%)

## How to Regenerate

```bash
# Run unit tests with coverage
cd /root/emergent/apps/server-go
unset LOG_LEVEL
go test ./domain/... ./internal/config/... ./internal/jobs/... ./internal/storage/... $(go list ./pkg/... | grep -v logger) \
  -count=1 -coverprofile=coverage_combined/unit_v3.out \
  -coverpkg=./domain/...,./pkg/...,./internal/...

# Run e2e tests with coverage
set -a && source ../../.env && source ../../.env.local && set +a
go test ./tests/e2e/... -count=1 -coverprofile=coverage_combined/e2e_v3.out \
  -coverpkg=./domain/...,./pkg/...,./internal/... -timeout 10m

# Merge coverage files
{
  echo "mode: set"
  tail -n +2 coverage_combined/unit_v3.out
  tail -n +2 coverage_combined/e2e_v3.out
} > coverage_combined/combined_v3.out

# View total coverage
go tool cover -func=coverage_combined/combined_v3.out | grep "total:"

# Count functions at 100%
go tool cover -func=coverage_combined/combined_v3.out | grep "100.0%" | wc -l

# Generate HTML report
go tool cover -html=coverage_combined/combined_v3.out -o coverage_combined/combined_v3.html
```

## Next Steps for Coverage Improvement

1. **Events Service Tests** - Add unit tests for Subscribe, Emit, EmitCreated, etc.
2. **Extraction Domain** - 46 functions at 0% - focus on business logic
3. **Datasource Domain** - 42 functions at 0%
4. **Email Templates** - 22 functions at 0% - template rendering is unit-testable
