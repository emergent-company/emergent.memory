---
description: 'Instructions for workspace management, including logging, process management, and running scripts.'
applyTo: '**'
---

# Coding Agent Instructions

## Domain-Specific Pattern Documentation

Before implementing new features, **always check** these domain-specific AGENT.md files to understand existing patterns and avoid recreating functionality:

### Frontend

| File                                                     | Domain              | Key Topics                                                                          |
| -------------------------------------------------------- | ------------------- | ----------------------------------------------------------------------------------- |
| `apps/admin/src/components/AGENT.md`                     | Frontend Components | Atomic design (atoms/molecules/organisms), DaisyUI + Tailwind, available components |
| `apps/admin/src/components/organisms/DataTable/AGENT.md` | DataTable Patterns  | Table configuration, columns, sorting, filtering, pagination                        |
| `apps/admin/src/contexts/AGENT.md`                       | React Contexts      | Auth, Theme, Toast, Modal contexts and provider patterns                            |
| `apps/admin/src/hooks/AGENT.md`                          | Frontend Hooks      | `useApi` (MUST use for all API calls), all 33+ hooks categorized                    |
| `apps/admin/src/pages/AGENT.md`                          | Page Patterns       | Route structure, page layouts, data loading patterns                                |

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

## EPF Strategy Context

The company-wide EPF strategy instance is at `docs/EPF/_instances/emergent/`.
This is a **git submodule** pointing to `emergent-company/emergent-epf`.

```bash
# If the directory is empty after cloning, initialize the submodule:
git submodule update --init

# To update to the latest strategy:
git submodule update --remote docs/EPF/_instances/emergent
```

Use EPF CLI MCP tools (epf-strategy server) with `instance_path: "docs/EPF/_instances/emergent"` for strategic context lookups, value model analysis, and feature-strategy alignment.

## 1. Logging

Logs are browsed using the **logs MCP server**. Log files are stored in `logs/` (root directory):

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

Services use PID-based process management via workspace CLI or **Workspace MCP server**.

### Hot Reload (Default) — DO NOT RESTART AFTER CODE CHANGES

**Both server (Go) and admin (Vite) have hot reload enabled.**

⚠️ **IMPORTANT: Never restart services after making code changes.** Hot reload automatically picks up changes within 1-2 seconds. Restarting takes ~30 seconds (Go) or ~1.5 minutes (NestJS) and is unnecessary.

**When hot reload works (DO NOT restart):**

- ✅ Editing Go files (handlers, services, stores, etc.)
- ✅ Editing React components, hooks, styles
- ✅ Modifying entities, utilities
- ✅ Changing business logic

**When to restart (rare cases only):**

- ❌ Adding NEW fx modules to `cmd/server/main.go`
- ❌ Changing environment variables
- ❌ After `go mod tidy` / `pnpm install`
- ❌ Server is not responding (check with `pnpm run workspace:status`)

### Checking Service Health

Before restarting, **always check if the server is actually down:**

```bash
pnpm run workspace:status    # Check if services are running
curl http://localhost:3002/health   # Direct health check
```

Only restart if `workspace:status` shows the service is offline or unhealthy.

### Commands

```bash
pnpm run workspace:status    # Check status (USE THIS FIRST)
pnpm run workspace:restart   # Restart all services (ONLY IF NEEDED)
pnpm run workspace:start     # Start all services
pnpm run workspace:stop      # Stop all services
```

**Common mistakes:**

| Wrong                                           | Right                                       |
| ----------------------------------------------- | ------------------------------------------- |
| Restarting after editing a service file         | Just save the file, hot reload handles it   |
| Restarting to "make sure changes are picked up" | Check logs to confirm hot reload worked     |
| `nx run server:build` then manually run         | Hot reload, or `pnpm run workspace:restart` |
| Killing processes with `kill -9`                | `pnpm run workspace:stop`                   |

## 3. Testing

See **`docs/testing/AI_AGENT_GUIDE.md`** for comprehensive guidance.

**Quick commands:**

```bash
nx run admin:test              # Frontend unit tests
nx run admin:test-coverage     # With coverage
nx run admin:e2e               # Browser e2e tests
nx run server-go:test          # Backend unit tests (Go)
nx run server-go:test-e2e      # API e2e tests (Go)
```

## 4. Custom Tools

- **credentials** - Get test user credentials and application URLs
- **open-browser** - Launch isolated browser with test credentials (Chromium preferred)

## 5. Available MCP Servers

MCP tool documentation is available via tool introspection. Key servers:

- **Postgres** - Database queries (use schema-qualified names: `kb.documents`, `core.user_profiles`)
- **Chrome DevTools** - Browser debugging (start with `npm run chrome:debug` first)
- **logs** - Log file browsing
- **Workspace** - Health monitoring, Docker logs, config
- **Langfuse** - AI trace browsing
- **SigNoz** - Observability (traces, logs, metrics, alerts)

## 6. Database Queries

**Always consult `docs/database/schema-context.md` first.**

```sql
-- Use schema-qualified names
SELECT * FROM kb.documents;           -- NOT 'documents'
SELECT * FROM core.user_profiles;     -- NOT 'users'
SELECT * FROM kb.object_extraction_jobs;
```

**Schemas:** `kb` (knowledge base), `core` (users), `public` (extensions)

## 7. Bug Reports & Improvements

- **Bugs:** `docs/bugs/` — Use `docs/bugs/TEMPLATE.md`
- **Improvements:** `docs/improvements/` — Use `docs/improvements/TEMPLATE.md`
