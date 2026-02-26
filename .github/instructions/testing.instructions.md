---
applyTo: '**'
---

# Testing Infrastructure - AI Assistant Instructions

## Overview

The backend is a pure Go server (`apps/server-go`). The React admin frontend lives in a separate repo at `/root/emergent.memory.ui`. Use `task` (Taskfile) for backend commands and `pnpm` for frontend commands.

**For detailed testing guidance and templates, see `docs/testing/AI_AGENT_GUIDE.md`** which provides:

- Test type decision trees (unit, integration, API e2e, browser e2e)
- Test templates and quick reference
- Directory structure and file naming conventions
- Import patterns and best practices

## Test Directory Structure

### Server (`apps/server-go`)

```
tests/
  ├── unit/               # Go unit tests
  ├── integration/        # Go integration tests
  └── e2e/                # Go API e2e tests
```

### Admin (repo: `/root/emergent.memory.ui`)

```
tests/
  ├── unit/               # Vitest unit tests
  └── e2e/                # Playwright browser e2e tests
```

## Running Tests

### Backend (Go)

```bash
# From repo root or apps/server-go
task test                          # Unit tests
task test:e2e                      # All E2E tests
task test:e2e -- -run GraphSuite   # Specific suite
task test:integration              # Integration tests
task test:coverage                 # With coverage report
```

### Frontend (emergent.memory.ui)

```bash
# cd /root/emergent.memory.ui
pnpm run test                      # Unit tests (Vitest)
pnpm run test:coverage             # With coverage
```

### Combined Regression

```bash
# Backend
task test
task test:e2e

# Frontend (cd /root/emergent.memory.ui)
pnpm run test
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

- **Backend:** `task test:coverage` → `apps/server-go/coverage.html`
- **Frontend:** `cd /root/emergent.memory.ui && pnpm run test:coverage`

## CI Alignment

GitHub Actions under `.github/workflows/` use task commands for backend tests. Frontend CI is in the `emergent.memory.ui` repo.

## Debugging Failures

### Playwright (Frontend repo)

**ALWAYS check Playwright logs and reports after test runs.**

```bash
# Open the interactive HTML report in browser
npx playwright show-report tests/e2e/test-results/html-report
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
- Add `-v` for verbose: `task test:e2e -- -v`
- Check `.go` test files for lingering database handles; close in `TestMain` or `TearDownSuite`

## Best Practices

### AI Assistants

1. Default to task commands for backend, pnpm for frontend.
2. Confirm Docker deps are running before advising E2E test runs.
3. **ALWAYS check test output and artifacts** before asking the user what went wrong.
4. Never parallelize Playwright specs unless suites are explicitly isolated.
5. For Go tests touching the database, require proper test suite setup/teardown.

### Developers

1. Run unit tests + lint before every commit.
2. Keep E2E runs deterministic: seed data or stub network responses inside tests.
3. Use `scripts/validate-story-duplicates.mjs` before Storybook work (frontend repo).
4. Document non-trivial test data builders in `docs/` for future contributors.

## Locating Tests

| Area               | Pattern                                         |
| ------------------ | ----------------------------------------------- |
| Server unit (Go)   | `apps/server-go/tests/**/*_test.go`             |
| Server integration | `apps/server-go/tests/integration/**/*_test.go` |
| Server e2e (Go)    | `apps/server-go/tests/e2e/**/*_test.go`         |
| Admin unit         | `/root/emergent.memory.ui/tests/unit/**/*.test.{ts,tsx}` |
| Admin e2e          | `/root/emergent.memory.ui/tests/e2e/specs/**/*.spec.ts`  |

## Quick Reference

| Task              | Command                                         | Notes                                    |
| ----------------- | ----------------------------------------------- | ---------------------------------------- |
| Server unit tests | `task test`                                     | Use `-- -run TestName` to filter         |
| Server E2E        | `task test:e2e`                                 | Requires Postgres + Zitadel running      |
| Server integration| `task test:integration`                         |                                          |
| Server coverage   | `task test:coverage`                            | Outputs coverage.html                    |
| Admin unit tests  | `cd /root/emergent.memory.ui && pnpm run test`  | Append `-- -t "name"` for focused run    |
| Server status     | `task status`                                   | Check if server is running               |

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
npx playwright install chromium
npx playwright install-deps
```

## Related Documentation

- `docs/testing/AI_AGENT_GUIDE.md`
- `docs/DEV_PROCESS_MANAGER.md`

## Remember

- Use `task` for all backend operations.
- Check `task status` before assuming the server is down.
- Tests should reflect user behavior; avoid mocking core integrations in E2E suites.
