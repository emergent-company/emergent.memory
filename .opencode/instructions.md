---
description: 'Instructions for workspace management, including logging, process management, and running scripts.'
applyTo: '**'
---

# Coding Agent Instructions

## Domain-Specific Pattern Documentation

Before implementing new features, **always check** these domain-specific AGENT.md files to understand existing patterns and avoid recreating functionality:

### Frontend (repo: `/root/emergent.memory.ui`)

> The React admin UI is in a **separate repo** at `/root/emergent.memory.ui` (`emergent-company/emergent.memory.ui`).

| File                                                                              | Domain              | Key Topics                                                                          |
| --------------------------------------------------------------------------------- | ------------------- | ----------------------------------------------------------------------------------- |
| `/root/emergent.memory.ui/src/components/AGENT.md`                               | Frontend Components | Atomic design (atoms/molecules/organisms), DaisyUI + Tailwind, available components |
| `/root/emergent.memory.ui/src/components/organisms/DataTable/AGENT.md`           | DataTable Patterns  | Table configuration, columns, sorting, filtering, pagination                        |
| `/root/emergent.memory.ui/src/contexts/AGENT.md`                                 | React Contexts      | Auth, Theme, Toast, Modal contexts and provider patterns                            |
| `/root/emergent.memory.ui/src/hooks/AGENT.md`                                    | Frontend Hooks      | `useApi` (MUST use for all API calls), all 33+ hooks categorized                    |
| `/root/emergent.memory.ui/src/pages/AGENT.md`                                    | Page Patterns       | Route structure, page layouts, data loading patterns                                |

### Backend

| File                                  | Domain              | Key Topics                                             |
| ------------------------------------- | ------------------- | ------------------------------------------------------ |
| `apps/server-go/AGENT.md`             | Go Server           | fx modules, Echo handlers, Bun ORM, job queues, ADK-Go |
| `apps/server-go/migrations/README.md` | Database Migrations | Goose workflow, creating migrations, rollbacks         |

### Maintenance

- **Update command**: Use `/update-agent-docs` to review and update all AGENT.md files
- **Changelog**: See `docs/AGENT_DOCS_CHANGELOG.md` for update history

## Environment URLs

| Environment | Admin URL                               | Server URL                            |
| ----------- | --------------------------------------- | ------------------------------------- |
| Local       | `http://localhost:5176`                 | `http://localhost:3002`               |
| Dev         | `https://admin.dev.emergent-company.ai` | `https://api.dev.emergent-company.ai` |

## 1. Logging

Log files are stored in `logs/` (root directory):

```
logs/
├── server/
│   ├── server.log          # Main server log (INFO+)
│   ├── server.error.log    # Server errors only
│   ├── server.debug.log    # Debug output (dev only)
│   └── server.http.log     # HTTP request/response logs
├── admin/
│   ├── admin.out.log       # Vite stdout
│   ├── admin.error.log     # Vite stderr
│   └── admin.client.log    # Browser client logs
```

**Common log queries:** "Check server logs", "Are there any errors?", "Search logs for 'TypeError'"

## 2. Process Management

Services use PID-based process management via **Taskfile tasks**.

### Hot Reload (Default) — DO NOT RESTART AFTER CODE CHANGES

**The Go server has hot reload enabled (air).**

⚠️ **IMPORTANT: Never restart services after making code changes.** Hot reload automatically picks up changes within 1-2 seconds.

**When hot reload works (DO NOT restart):**

- ✅ Editing Go files (handlers, services, stores, etc.)
- ✅ Modifying entities, utilities
- ✅ Changing business logic

**When to restart (rare cases only):**

- ❌ Adding NEW fx modules to `cmd/server/main.go`
- ❌ Changing environment variables
- ❌ After `go mod tidy`
- ❌ Server is not responding (check with `task status`)

### Checking Service Health

Before restarting, **always check if the server is actually down:**

```bash
task status                        # Check if server is running
curl http://localhost:3002/health  # Direct health check
```

Only restart if the server is offline or unhealthy.

### Commands

```bash
task status      # Check server status (USE THIS FIRST)
task dev         # Start with hot reload (air, foreground)
task start       # Build + start in background (writes PID)
task stop        # Stop background server
```

**Common mistakes:**

| Wrong                                           | Right                                       |
| ----------------------------------------------- | ------------------------------------------- |
| Restarting after editing a service file         | Just save the file, hot reload handles it   |
| Restarting to "make sure changes are picked up" | Check logs to confirm hot reload worked     |
| `nx run server-go:build` then manually run      | `task dev` (hot reload) or `task start`     |
| Killing processes with `kill -9`                | `task stop`                                 |

## 3. Testing

See **`docs/testing/AI_AGENT_GUIDE.md`** for comprehensive guidance.

**Quick commands:**

```bash
# Backend (run from repo root or apps/server-go)
task test                      # Backend unit tests (Go)
task test:e2e                  # API e2e tests (Go)
task test:integration          # Integration tests (Go)
task test:coverage             # Tests with coverage report

# Frontend (cd /root/emergent.memory.ui)
pnpm run test                  # Frontend unit tests
```

## 4. Observability (OTel Tracing)

Tracing is **opt-in** — set `OTEL_EXPORTER_OTLP_ENDPOINT` to enable. Without it the server uses a no-op provider (zero overhead).

### Start Tempo

```bash
docker compose --profile observability up tempo -d
```

Add to `.env.local`:
```
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
OTEL_SERVICE_NAME=emergent-server
```

### Query traces

```bash
emergent traces list                      # Recent traces (last 1h)
emergent traces list --since 30m          # Last 30 min
emergent traces search --service emergent-server
emergent traces search --route /api/kb/documents --min-duration 200ms
emergent traces get <traceID>             # Full span tree
```

Data retention defaults to 720 h (30 days). Override with `OTEL_RETENTION_HOURS` in `.env.local`.

## 5. Custom Tools

- **credentials** - Get test user credentials and application URLs
- **open-browser** - Launch isolated browser with test credentials (Chromium preferred)

## 6. Available MCP Servers

MCP tool documentation is available via tool introspection. Key servers:

- **Postgres** - Database queries (use schema-qualified names: `kb.documents`, `core.user_profiles`)
- **Chrome DevTools** - Browser debugging (start with `./scripts/start-chrome-debug.sh` first)
- **SigNoz** - Observability (traces, logs, metrics, alerts)

## 7. Database Queries

**Always consult `docs/database/schema-context.md` first.**

```sql
-- Use schema-qualified names
SELECT * FROM kb.documents;           -- NOT 'documents'
SELECT * FROM core.user_profiles;     -- NOT 'users'
SELECT * FROM kb.object_extraction_jobs;
```

**Schemas:** `kb` (knowledge base), `core` (users), `public` (extensions)

## 8. Bug Reports & Improvements

- **Bugs:** `docs/bugs/` — Use `docs/bugs/TEMPLATE.md`
- **Improvements:** `docs/improvements/` — Use `docs/improvements/TEMPLATE.md`
