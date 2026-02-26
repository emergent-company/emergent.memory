
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

## Detailed Documentation

- **Workspace operations**: `.opencode/instructions.md` (logging, process management)
- **Testing guide**: `docs/testing/AI_AGENT_GUIDE.md`
- **Database schema**: `docs/database/schema-context.md`
