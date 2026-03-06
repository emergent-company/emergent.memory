# E2E Testing Reference

How to run and configure E2E and integration tests for the emergent server-go app.

---

## Environment Variables

All tests use the `testutil.BaseSuite`. DB connection is configured via:

| Variable | Default | Local dev value |
|----------|---------|-----------------|
| `POSTGRES_HOST` | `localhost` | `127.0.0.1` |
| `POSTGRES_PORT` | `5432` | **`5436`** |
| `POSTGRES_USER` | `emergent` | `emergent` |
| `POSTGRES_PASSWORD` | _(empty)_ | `local-test-password` |
| `POSTGRES_DB` | `emergent` | `emergent` |

Always set `POSTGRES_PORT=5436` locally — the dev stack uses a non-standard port.

## Running Tests

**All E2E tests (in-process server):**
```bash
cd apps/server-go
POSTGRES_PORT=5436 POSTGRES_PASSWORD=local-test-password go test ./tests/e2e/...
```

**Single suite:**
```bash
POSTGRES_PORT=5436 POSTGRES_PASSWORD=local-test-password go test ./tests/e2e/ -run TestAuthSuite
```

**Against external server (e.g., local running server):**
```bash
TEST_SERVER_URL=http://localhost:3012 POSTGRES_PORT=5436 POSTGRES_PASSWORD=local-test-password \
  go test ./tests/e2e/...
```

**Integration tests:**
```bash
POSTGRES_PORT=5436 POSTGRES_PASSWORD=local-test-password go test ./tests/integration/...
```

## How It Works

- Without `TEST_SERVER_URL`: tests spin up an **in-process Go test server** with an isolated DB per suite
- With `TEST_SERVER_URL`: tests hit a running external server; DB connection optional (only needed for direct DB assertions)
- Each test runs in a transaction that is rolled back after the test — fast cleanup, no stale data

## Test Tokens (Static Dev Tokens)

| Token | Maps to |
|-------|---------|
| `e2e-test-user` | AdminUser fixture (`test-admin-user`) — **no OrgID** |

**Important:** `e2e-test-user` has no `OrgID`. Any operation that requires `organization_id NOT NULL` will fail with a constraint violation. Use `test-org-user` (if available) or ensure the column allows NULL for E2E test compatibility.

## Common Failures

| Symptom | Cause | Fix |
|---------|-------|-----|
| `connection refused` on port 5432 | Wrong port | Set `POSTGRES_PORT=5436` |
| `organization_id NOT NULL violation` | e2e-test-user has no OrgID | Make column nullable or use a different test fixture |
| Goose migration errors | Missing env vars | Set all POSTGRES_* vars before running |

## Goose Migrations

```bash
# Run from apps/server-go
export POSTGRES_PORT=5436 POSTGRES_PASSWORD=local-test-password

# Status
goose -dir internal/db/migrations postgres \
  "host=127.0.0.1 port=5436 user=emergent password=local-test-password dbname=emergent sslmode=disable" status

# Up
goose -dir internal/db/migrations postgres \
  "host=127.0.0.1 port=5436 user=emergent password=local-test-password dbname=emergent sslmode=disable" up

# Down (one step)
goose -dir internal/db/migrations postgres \
  "host=127.0.0.1 port=5436 user=emergent password=local-test-password dbname=emergent sslmode=disable" down
```
