---
description: Run all implemented tests in the project (unit, E2E, and coverage)
mode: agent
---

# Run All Tests in Spec Server Project

> **LEGACY**: This prompt predates the monorepo migration. The React/Vite admin, nx workspace, and npm workspace commands were removed. Testable apps are now `apps/server` (Go) and `apps/web-ui` (Go templ + HTMX gateway + Playwright). Current commands: `task test`, `task test:integration`, `task test:e2e` (API e2e in `e2e/tests-api/`), `task lint`, and `cd apps/web-ui && task e2e:test`. See root `AGENTS.md`, `apps/server/AGENT.md`, `e2e/AGENTS.md`, and `apps/web-ui/tests/e2e/README.md`.

Execute the complete test suite across all applications in this monorepo. This prompt guides you through running unit tests, E2E tests, and generating coverage reports for backend and web UI.

## Project Structure

This is a monorepo with the following testable applications:

1. **Web UI** (`apps/web-ui/`) - Go templ + HTMX gateway + Playwright (E2E)
2. **Server Backend** (`apps/server/`) - Go (unit & integration tests), plus API e2e suites in `e2e/tests-api/`

## Test Execution Order

Follow this sequence to run all tests:

### 1. Server Backend Tests

#### Unit Tests

```bash
task test
```

#### Integration Tests

```bash
task test:integration
```

#### API E2E Suites (separate Go module)

```bash
task test:e2e
# Target a specific suite
task test:e2e -- -run TestGraphSuite
```

#### Coverage Report

```bash
task server:test:coverage
```

### 2. Web UI Tests

#### Playwright E2E

```bash
cd apps/web-ui
task e2e:test
```

## Prerequisites

Before running E2E tests, ensure these services are running:

- **PostgreSQL** (port 5432) - Database (dev default; the e2e Docker stack uses 5436)
- **Zitadel** (port 8080) - Auth server
- **API server** - local direct `http://localhost:3012`, or `http://localhost:3002` via SSH tunnel to the remote test server
- **Web UI gateway** - `task dev` from `apps/web-ui`

### Check Service Status

```bash
task status
```

### Start Required Services

```bash
task dev                       # Start API server with hot reload
cd apps/web-ui && task dev     # Start web UI gateway with hot reload
```

### API E2E Dependencies

The API e2e suites (`e2e/tests-api/`) bring up their own Docker stack; no npm dependency script is required.

## Full Test Suite Script

Here's a complete bash script that runs all tests sequentially:

```bash
set -e  # Exit on first failure

echo "🧪 Starting Complete Test Suite..."
echo ""

echo "1️⃣ Running Server Unit Tests..."
task test
echo "✅ Server unit tests passed"
echo ""

echo "2️⃣ Running Server Integration Tests..."
task test:integration
echo "✅ Server integration tests passed"
echo ""

echo "3️⃣ Running API E2E Suites..."
task test:e2e
echo "✅ API e2e tests passed"
echo ""

echo "4️⃣ Running Web UI Playwright E2E..."
cd apps/web-ui && task e2e:test
cd ../..
echo "✅ Web UI E2E tests passed"
echo ""

echo "🎉 All tests passed successfully!"
```

## Test Results and Coverage

After running tests with coverage, view the HTML reports:

### Server Coverage

```bash
task server:test:coverage   # Outputs apps/server/coverage.html
```

## Debugging Failed Tests

### Playwright E2E Failures

When Playwright tests fail, check the HTML report via `task e2e:report` from `apps/web-ui` (see `apps/web-ui/tests/e2e/README.md`).

### Go Test Failures

Run failed tests with verbose output:

```bash
# Server (Go), from apps/server
go test ./... -v -run "TestFailingName"

# API e2e, from repo root
task test:e2e -- -run TestGraphSuite -v
```

## Quick Commands

### Run Only Unit Tests

```bash
task test
```

### Run Only E2E Tests

```bash
task test:e2e                              # API e2e suites
cd apps/web-ui && task e2e:test            # Web UI Playwright e2e
```

### Generate All Coverage Reports

```bash
task server:test:coverage
```

## Important Notes

⚠️ **Always use the scripted `task` targets** – they wrap dependency checks and environment setup.

⚠️ **Don't run E2E tests in parallel** - They share database state and can interfere with each other.

✅ **Sequential execution recommended** - Run tests in the order specified above for best results.

## Related Documentation

For more details, see:

- [Testing Infrastructure Instructions](../instructions/testing.instructions.md)
- [Server agent guide](../../apps/server/AGENT.md)
- [E2E agent guide](../../e2e/AGENTS.md)
- [Web UI gateway guide](../../apps/web-ui/gateway/AGENTS.md)
- [Web UI e2e README](../../apps/web-ui/tests/e2e/README.md)
