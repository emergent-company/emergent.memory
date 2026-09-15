---
applyTo: '**'
---

# Testing Infrastructure - AI Assistant Instructions

## Overview

The backend is a pure Go server (`apps/server`). The web UI is the Go templ + HTMX gateway at `apps/web-ui`. Use `task` (Taskfile) for all backend and web UI commands.

**For detailed testing guidance and templates, see `docs/testing/AI_AGENT_GUIDE.md`** which provides:

- Test type decision trees (unit, integration, API e2e, browser e2e)
- Test templates and quick reference
- Directory structure and file naming conventions
- Import patterns and best practices

## Test Directory Structure

### Server (`apps/server`)

```
apps/server/tests/
  └── integration/        # Go integration tests (also *_test.go next to source)
```

API e2e suites live in a separate Go module: `e2e/tests-api/`.

### Web UI (`apps/web-ui`)

```
apps/web-ui/tests/e2e/    # Playwright browser e2e tests
```

## Running Tests

### Backend (Go)

```bash
# From repo root
task test                              # Unit tests
task test:e2e                          # API e2e suites (e2e/tests-api)
task test:e2e -- -run TestGraphSuite   # Specific API e2e suite
task test:integration                  # Integration tests
```

### Web UI (`apps/web-ui`)

```bash
cd apps/web-ui
task e2e:test                      # Playwright e2e (session mode against running gateway)
```

### Combined Regression

```bash
# Backend
task test
task test:e2e
task test:integration

# Web UI (cd apps/web-ui)
task e2e:test
```

## Dependency Management for Tests

Ensure Docker services (Postgres, Zitadel) are running before E2E tests:

```bash
docker compose -f docker/e2e/docker-compose.yml up -d  # Start e2e deps
task status                                              # Check server health
task dev                                                 # Start server if needed
```

Ports: Postgres 5432, Zitadel 8080, API 3002.

### Environment Variables

- `E2E_REAL_LOGIN=1` — use real Zitadel auth instead of mock tokens
- Database env defaults come from `.env`; override via shell when needed

## Coverage Reports

- **Backend:** `task server:test:coverage` → `apps/server/coverage.html`

## CI Alignment

GitHub Actions under `.github/workflows/` use task commands for backend and web UI tests.

## Debugging Failures

### Playwright (Web UI)

**ALWAYS check Playwright logs and reports after test runs.**

```bash
# Web UI Playwright reports (cd apps/web-ui)
task e2e:report
```

The HTML report contains screenshots, traces, network calls, and console logs.

After any Playwright test run, IMMEDIATELY check the HTML report:

1. **First**: Open and examine the HTML report
2. **Check**: Screenshot shows what page actually rendered
3. **Check**: Network tab shows API calls and status codes
4. **Check**: Console shows JavaScript errors
5. **Check**: Trace shows exact DOM state when test failed
6. **Then**: Diagnose root cause from artifacts (don't guess or ask user)

### Go Tests

- Use `-run TestName` to filter: `task test -- -run TestMyFunction`
- Add `-v` for verbose: `task test:e2e -- -v` (API e2e suites are named `TestGraphSuite`, `TestSearchSuite`, `TestDocumentsSuite`, `TestChunksSuite`, `TestHealthSuite`, `TestOrgsSuite`, `TestProjectsSuite`)
- Check `.go` test files for lingering database handles; close in `TestMain` or `TearDownSuite`

## Best Practices

### AI Assistants

1. Default to `task` commands for backend and web UI.
2. Confirm Docker deps are running before advising E2E test runs.
3. **ALWAYS check test output and artifacts** before asking the user what went wrong.
4. Never parallelize Playwright specs unless suites are explicitly isolated.
5. For Go tests touching the database, require proper test suite setup/teardown.

### Developers

1. Run unit tests + lint before every commit.
2. Keep E2E runs deterministic: seed data or stub network responses inside tests.
3. For web UI e2e, follow the conventions in `apps/web-ui/tests/e2e/README.md`.
4. Document non-trivial test data builders in `docs/` for future contributors.

## Locating Tests

| Area               | Pattern                                         |
| ------------------ | ----------------------------------------------- |
| Server unit (Go)   | `apps/server/**/*_test.go`                      |
| Server integration | `apps/server/tests/integration/**/*_test.go`    |
| API e2e (Go)       | `e2e/tests-api/suites/*_test.go`                |
| Web UI e2e         | `apps/web-ui/tests/e2e/**/*.spec.ts`            |

## Quick Reference

| Task               | Command                      | Notes                                                       |
| ------------------ | ---------------------------- | ----------------------------------------------------------- |
| Server unit tests  | `task test`                  | Use `-- -run TestName` to filter                            |
| API e2e suites     | `task test:e2e`              | Runs `e2e/tests-api` (separate Go module)                   |
| Server integration | `task test:integration`      | `apps/server/tests/integration/`                            |
| Server coverage    | `task server:test:coverage`  | Outputs coverage.html                                       |
| Web UI e2e         | `cd apps/web-ui && task e2e:test` | Playwright, session mode against running gateway        |
| Server status      | `task status`                | Check if server is running                                  |

## Troubleshooting

### Ports Busy

```bash
lsof -ti:3002,5432,8080 | xargs kill -9
docker compose -f docker/e2e/docker-compose.yml restart
```

### Database Connection Errors

```bash
docker compose -f docker/e2e/docker-compose.yml restart
task status
```

### Playwright Browser Missing

```bash
cd apps/web-ui && task e2e:install
```

## Related Documentation

- `docs/testing/AI_AGENT_GUIDE.md`
- `docs/DEV_PROCESS_MANAGER.md`

## Remember

- Use `task` for all backend operations.
- Check `task status` before assuming the server is down.
- Tests should reflect user behavior; avoid mocking core integrations in E2E suites.
