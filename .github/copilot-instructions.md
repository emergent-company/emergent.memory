# Coding Agent Instructions

This document provides instructions for interacting with the workspace, including logging, process management, and running scripts.

## Domain-Specific Pattern Documentation

Before implementing new features, **always check** these domain-specific AGENT.md files to understand existing patterns and avoid recreating functionality:

| File                                  | Domain          | Key Topics                                                          |
| ------------------------------------- | --------------- | ------------------------------------------------------------------- |
| `apps/server/AGENT.md`                | Go Backend      | fx modules, Echo handlers, Bun ORM, job queues                      |
| `apps/web-ui/gateway/AGENTS.md`       | Web UI (Go/HTMX)| Go templ + HTMX gateway guidelines, verify commands                |
| `apps/web-ui/tests/e2e/README.md`     | Web UI E2E      | Playwright projects, auth, helpers, test-id convention              |
| `e2e/AGENTS.md`                       | CLI / API E2E   | Go API e2e suites (`e2e/tests-api/`) and CLI runlog suite           |

> **Monorepo layout**: The Go API lives in `apps/server/`; the web UI (Go templ + HTMX gateway + Playwright e2e) lives in `apps/web-ui/`; the CLI in `apps/cli/`; connectors in `apps/connector.linux` and `apps/connector.mac`; iOS in `apps/ios/`. The old React/Vite admin and NestJS server are gone.

**When to read these files:**

- Before creating new UI pages/components → Read `apps/web-ui/gateway/AGENTS.md`
- Before writing Playwright e2e → Read `apps/web-ui/tests/e2e/README.md`
- Before adding API e2e suites → Read `e2e/AGENTS.md`
- Before creating new API endpoints or database entities → Read `apps/server/AGENT.md`

## Primary References

- **`AGENTS.md`** (root) - Quick reference for build, lint, test, and pattern links
- **`.opencode/instructions.md`** - Workspace operations (logging, process management, testing)
- **`docs/testing/AI_AGENT_GUIDE.md`** - Comprehensive testing guidance

## 1. Logging

Log files are stored in `logs/` (root directory).

- **Server logs:** `logs/server/server.log`, `logs/server/server.error.log`

## 2. Process Management

Services use **Taskfile tasks** for process management.

- **Start with hot reload (foreground):**
  ```bash
  task dev
  ```

- **Start in background:**
  ```bash
  task start
  ```

- **Stop background server:**
  ```bash
  task stop
  ```

- **Check server status:**
  ```bash
  task status
  ```

The Go server uses `air` for hot reload. **Do not restart after code changes** — changes are picked up automatically.

## 3. Running Scripts and Tests

All backend tasks use `task` (Taskfile):

```bash
task build              # Build server binary
task test               # Unit tests
task test:e2e           # API e2e tests
task test:integration   # Integration tests
task server:test:coverage # Tests with coverage
task lint               # Go linter
task migrate:up         # Run migrations
```

For web UI tasks, use `task` in `apps/web-ui` (Go templ + HTMX gateway, Playwright e2e):

```bash
cd apps/web-ui
task dev                # Gateway with air hot reload
task lint               # ruff + golangci-lint + go vet/test + templ + gitleaks
task e2e:test           # Playwright e2e against the running gateway
```

For comprehensive testing guidance, refer to **`docs/testing/AI_AGENT_GUIDE.md`**.

## 4. EPF Working Directory

When working with the Emergent Product Framework (EPF), **all temporary working documents** (analysis, summaries, session notes, implementation docs) go in:

```
docs/EPF/.epf-work/
```

**Key Rules:**

- ✅ **DO** create subdirectories by session/topic: `docs/EPF/.epf-work/skattefunn-wizard-selection/`
- ✅ **DO** place ALL EPF-related working documents there (summaries, analysis, decisions, session notes)
- ❌ **DON'T** create `.epf-work/` at repository root
- ❌ **DON'T** create `.epf-work/` inside `_instances/`
- ❌ **DON'T** place working documents in canonical EPF directories (schemas, wizards, templates)

**Why one location?**

- Single source of truth for all EPF working documents
- Easy to find session notes and analysis
- Clear separation: `docs/EPF/` = canonical framework, `docs/EPF/.epf-work/` = temporary work
- Version-controlled with EPF for context preservation

See `docs/EPF/.github/copilot-instructions.md` for complete EPF contribution guidelines.
