# Memory

Go monorepo for the Memory knowledge graph platform. React admin UI lives in a **separate repo** at `/root/emergent.memory.ui`.

## Architecture

**Go module:** `github.com/emergent-company/emergent.memory`

**Server stack:** Echo (HTTP) · Bun ORM (pgx/Postgres) · fx (dependency injection) · Zitadel (auth)

**Domain layout** (`apps/server/domain/<name>/`): agents, apitoken, authinfo, backups, branches, chat, chunking, chunks, datasource, devtools, discoveryjobs, docs, documents, email, embeddingpolicies, events, extraction, githubapp, graph, health, integrations, invites, mcp, mcpregistry, monitoring, notifications, orgs, projects, provider, scheduler, search, standalone, superadmin, tasks, templatepacks, tracing, typeregistry, useraccess, useractivity, userprofile, users, workspace, workspaceimages

Each domain: `handler.go` (Echo routes) · `service.go` (business logic) · `store.go` (Bun ORM queries) · `module.go` (fx wiring)

**DB:** Postgres on port `5436` (not 5432) · schemas: `kb` (knowledge), `core` (users/orgs) · migrations in `apps/server/migrations/` via Goose

**CLI:** source at `tools/cli/` · install with `task cli:install` → `~/.local/bin/memory` · defaults to remote `http://mcj-emergent:3002`; override with `--server http://localhost:3012`



```bash
# Backend (repo root or apps/server)
task build          # build Go server binary
task test           # unit tests
task test:e2e       # API e2e tests
task lint           # Go linter
task cli:install    # build + install memory CLI → ~/.local/bin/memory

# Frontend (/root/emergent.memory.ui)
pnpm run lint
pnpm run test
```

## Hot Reload — DO NOT restart after code changes

The Go server uses `air`. Changes are picked up in 1-2 seconds automatically.

- **Just save the file** — hot reload handles Go handler/service/store changes
- **Restart only for**: new fx modules in `cmd/server/main.go`, env var changes, after `go mod tidy`, server down
- **After structural refactors** (new packages, moved types): restart `air` explicitly — incremental reload may silently use stale binary
- **Confirm reload worked**: check `logs/server/server.log` for a fresh startup line before testing

```bash
task status      # check server health first
task dev         # start with hot reload (foreground)
task start       # build + start in background
task stop        # stop background server
```

## Environment URLs

| | Admin | Server |
|---|---|---|
| Domain (preferred) | `https://admin.dev.emergent-company.ai` | `https://api.dev.emergent-company.ai` |
| Localhost | `http://localhost:5176` | `http://localhost:5300` |
| mcj-emergent (remote test) | — | `http://localhost:3002` (via SSH tunnel) |
| Local Go server (direct) | — | `http://localhost:3012` |

## Before Writing Code — Check These First

| Creating… | Read first… |
|---|---|
| React component | `/root/emergent.memory.ui/src/components/AGENT.md` — 50+ components |
| React hook | `/root/emergent.memory.ui/src/hooks/AGENT.md` — use `useApi` for ALL API calls |
| Go endpoint | `apps/server/AGENT.md` — fx modules, Echo, Bun ORM |
| Database entity | `apps/server/AGENT.md` — Bun models, kb/core schemas |

Common mistakes: raw `fetch()` calls (use `useApi`), creating components that already exist.

## Code Style

- **Go**: `gofmt`, no unused imports, wrap errors: `fmt.Errorf("context: %w", err)`
- **TypeScript**: strict, no `any`
- **Database**: always schema-qualified — `kb.documents`, `core.user_profiles`

## Observability

Tracing is opt-in. Set `OTEL_EXPORTER_OTLP_ENDPOINT` to enable (no-op when unset).

```bash
docker compose --profile observability up tempo -d  # start Tempo
memory traces list --since 30m                    # query traces
memory traces get <traceID>                       # full span tree
```

## Logs

```
logs/server/server.log        logs/server/server.error.log
logs/admin/admin.out.log      logs/admin/admin.error.log
```

## Gotchas

- `docs/site/` is tracked in git — do NOT add to `.gitignore`
- `search/client_test.go` and `health/client_test.go` have pre-existing compile errors; ignore unless working on those packages
- **SSH timeouts**: SSH commands to `mcj-emergent` time out at ~120s. For long operations (builds, test suites), run in background: `ssh root@mcj-emergent "nohup <cmd> > /tmp/out.log 2>&1 &"`. Use `gh run watch` to track CI instead of polling manually.
- **Ephemeral container deploys**: copying a binary into a running Docker container is temporary — a container restart reverts to the image version. All permanent deployments require a tagged release pushed through CI.

## Detail Docs

| File | Contents |
|------|----------|
| `apps/server/AGENT.md` | fx modules, Echo handlers, Bun ORM, job queues |
| `apps/server/migrations/README.md` | Goose migration workflow |
| `/root/emergent.memory.ui/src/components/AGENT.md` | 50+ components, atomic design, DaisyUI |
| `/root/emergent.memory.ui/src/components/organisms/DataTable/AGENT.md` | DataTable config, columns, sorting |
| `/root/emergent.memory.ui/src/contexts/AGENT.md` | Auth, Theme, Toast, Modal contexts |
| `/root/emergent.memory.ui/src/hooks/AGENT.md` | 33+ hooks, `useApi` patterns |
| `/root/emergent.memory.ui/src/pages/AGENT.md` | route structure, page layouts |
| `docs/testing/AI_AGENT_GUIDE.md` | full testing guide |
| `docs/database/schema-context.md` | DB schema reference |
