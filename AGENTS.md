
# AI Agent Instructions

## ⚠️ STOP — Check Existing Patterns First

**Before creating ANY new code, you MUST check if similar functionality already exists.**

| Creating...     | First read...                              | Contains...                                                    |
| --------------- | ------------------------------------------ | -------------------------------------------------------------- |
| React component | `/root/emergent.memory.ui/src/components/AGENT.md` | 50+ components, atomic design, DaisyUI patterns        |
| React hook      | `/root/emergent.memory.ui/src/hooks/AGENT.md`      | 33+ hooks including `useApi` (REQUIRED for API calls)  |
| Go endpoint     | `apps/server-go/AGENT.md`                  | fx modules, Echo handlers, Bun ORM, job queues                 |
| Database entity | `apps/server-go/AGENT.md`                  | Bun models, schemas (kb/core), migrations                      |
| Backend tests   | `apps/server-go/AGENT.md`                  | E2E test patterns, testutil helpers                            |

> **Frontend repo**: The React admin lives at `/root/emergent.memory.ui` (remote: `emergent-company/emergent.memory.ui`). It is a standalone Vite project — not in this monorepo.

**Common mistakes to avoid:**

- ❌ Creating a new data fetching hook → Use `useApi` from the frontend repo's hooks/AGENT.md
- ❌ Creating a new Button component → Use existing from the frontend repo's components/AGENT.md
- ❌ Raw `fetch()` calls → Use `useApi` hook with proper error handling
- ❌ New modal component → Use existing `Modal` atom

## Quick Reference

### Build, Lint, Test

```bash
# Backend (this repo) — run from repo root or apps/server-go
task build                      # Build Go server binary
task test                       # Backend unit tests (Go)
task test:e2e                   # API e2e tests (Go)
task lint                       # Run Go linter

# Frontend (emergent.memory.ui repo — cd /root/emergent.memory.ui)
pnpm run lint                   # Lint frontend
pnpm run test                   # Frontend unit tests
```

### Code Style

- **Go**: `gofmt`, no unused imports, follow existing patterns
- **TypeScript** (frontend): Strict types, no `any`
- **Naming**: `camelCase` variables/functions, `PascalCase` classes/interfaces

### ⚠️ Hot Reload — DO NOT RESTART AFTER CODE CHANGES

**The Go server has hot reload.** Changes are picked up automatically in 1-2 seconds.

- ✅ **Just save the file** — hot reload handles Go handler, service, and store changes
- ❌ **Only restart if** server is down (check with `task status`)
- ❌ **Restart required for:** new fx modules in `cmd/server/main.go`, env var changes, after `go mod tidy`

## Environment URLs

Local and Dev refer to the **same environment** accessible via two methods. Prefer domain URLs for consistency with production patterns.

| Access Method      | Admin URL                               | Server URL                            |
| ------------------ | --------------------------------------- | ------------------------------------- |
| Domain (preferred) | `https://admin.dev.emergent-company.ai` | `https://api.dev.emergent-company.ai` |
| Localhost          | `http://localhost:5176`                 | `http://localhost:3002`               |

## Observability (OTel Tracing)

Tracing is **opt-in**. When `OTEL_EXPORTER_OTLP_ENDPOINT` is unset the server runs with a no-op provider (zero overhead).

### Enable local tracing

```bash
# Start Grafana Tempo (single container, local storage)
docker compose --profile observability up tempo -d

# Add to .env.local
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
OTEL_SERVICE_NAME=emergent-server
OTEL_SAMPLING_RATE=1.0
```

### Query traces via CLI

```bash
emergent traces list                     # Recent traces (last 1h)
emergent traces list --since 30m         # Last 30 minutes
emergent traces search --service emergent-server --route /api/kb/documents
emergent traces search --min-duration 500ms
emergent traces get <traceID>            # Full span tree for a trace
```

Data is retained for 720 h (30 days) by default. Override with `OTEL_RETENTION_HOURS` in `.env.local`.

## Available Skills

Skills extend what you can do as an agent. They are defined in `.claude/skills/` and invoked via the `skill` tool (e.g., `skill: "openspec-new-change"`). **Always invoke a relevant skill before generating a response about that task.**

### OpenSpec Skills (Structured Change Workflow)

| Skill | When to use |
|-------|-------------|
| `openspec-new-change` | Start a new feature, fix, or modification using the artifact-driven workflow |
| `openspec-ff-change` | Fast-forward through all artifact creation steps at once |
| `openspec-continue-change` | Progress an existing change by creating the next artifact |
| `openspec-apply-change` | Implement tasks from a change (start or continue coding) |
| `openspec-verify-change` | Validate that implementation matches change artifacts before archiving |
| `openspec-archive-change` | Archive a single completed change |
| `openspec-bulk-archive-change` | Archive multiple completed changes at once |
| `openspec-sync-specs` | Sync delta specs from a change into main specs without archiving |
| `openspec-explore` | Enter thinking-partner mode to explore ideas and clarify requirements |
| `openspec-onboard` | Guided walkthrough of a complete OpenSpec workflow cycle |

### Other Skills

| Skill | When to use |
|-------|-------------|
| `commit` | Stage, commit, and push changes (conventional commits, auto-.gitignore) |
| `release` | Cut a versioned release — bump VERSION, tag, push to trigger CI |
| `deploy-mcj` | Upgrade the `mcj-emergent` test server via SSH |

## Detailed Documentation

- **Workspace operations**: `.opencode/instructions.md` (logging, process management)
- **Testing guide**: `docs/testing/AI_AGENT_GUIDE.md`
- **Database schema**: `docs/database/schema-context.md`
