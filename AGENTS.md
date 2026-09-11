# Memory

Go monorepo for the Memory knowledge graph platform. The web UI lives in a **separate repo** at `/root/memory.web-ui` (Go templ + HTMX gateway, plus the Playwright e2e suite — not a React app).

## Architecture

**Go module:** `github.com/emergent-company/emergent.memory`

**Server stack:** Echo (HTTP) · Bun ORM (pgx/Postgres) · fx (dependency injection) · Zitadel (auth)

**Domain layout** (`apps/server/domain/<name>/`): agents, apitoken, authinfo, backups, branches, chat, chunking, chunks, datasource, devtools, discoveryjobs, docs, documents, email, embeddingpolicies, events, extraction, githubapp, graph, health, integrations, invites, mcp, mcpregistry, monitoring, notifications, orgs, projects, provider, sandbox, sandboximages, scheduler, schemas, schemaregistry, search, skills, standalone, superadmin, tasks, tracing, useraccess, useractivity, userprofile, users

Each domain: `handler.go` (Echo routes) · `service.go` (business logic) · `store.go` (Bun ORM queries) · `module.go` (fx wiring)

**DB:** Postgres on port `5436` (not 5432) · schemas: `kb` (knowledge), `core` (users/orgs) · migrations in `apps/server/migrations/` via Goose

**CLI:** source at `tools/cli/` · install with `task cli:install` → `~/.memory/bin/memory` · defaults to `http://localhost:3012`; override with `--server <url>`



```bash
# Backend (repo root or apps/server)
task build          # build Go server binary
task test           # unit tests
task test:e2e       # API e2e tests
task lint           # Go linter
task cli:install    # build + install memory CLI → ~/.memory/bin/memory

# Web UI (/root/memory.web-ui) — Go templ + HTMX gateway
cd /root/memory.web-ui
task dev        # gateway with air hot reload (templ + tailwind + go)
task lint       # lefthook: ruff, golangci-lint, go vet/test, templ, gitleaks
task e2e:test   # Playwright e2e (see tests/e2e/README.md)
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
| Remote test server | — | `http://localhost:3002` (via SSH tunnel) |
| Local Go server (direct) | — | `http://localhost:3012` |

## Before Writing Code — Check These First

| Creating… | Read first… |
|---|---|
| Go templ page/component | `/root/memory.web-ui/gateway/AGENTS.md` — gateway Go/templ guidelines |
| E2E test (Playwright) | `/root/memory.web-ui/tests/e2e/README.md` — projects, auth, helpers, test-id convention |
| Go endpoint | `apps/server/AGENT.md` — fx modules, Echo, Bun ORM |
| Database entity | `apps/server/AGENT.md` — Bun models, kb/core schemas |

Common mistakes: hand-editing generated `*_templ.go` (run `templ generate`), skipping the gateway `go build`/`task lint`, and brittle e2e selectors (prefer `getByRole`/`name=` plus the gateway `data-testid` convention).

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
- **SSH timeouts**: SSH commands to remote servers time out at ~120s. For long operations (builds, test suites), run in background: `ssh root@your-server "nohup <cmd> > /tmp/out.log 2>&1 &"`. Use `gh run watch` to track CI instead of polling manually.
- **Ephemeral container deploys**: copying a binary into a running Docker container is temporary — a container restart reverts to the image version. All permanent deployments require a tagged release pushed through CI.

## Detail Docs

| File | Contents |
|------|----------|
| `apps/server/AGENT.md` | fx modules, Echo handlers, Bun ORM, job queues |
| `apps/server/migrations/README.md` | Goose migration workflow |
| `/root/memory.web-ui/gateway/AGENTS.md` | gateway Go/templ guidelines + verify commands |
| `/root/memory.web-ui/tests/e2e/README.md` | Playwright e2e suite: projects, auth, coverage, test-id convention |
| `docs/testing/AI_AGENT_GUIDE.md` | full testing guide |
| `docs/database/schema-context.md` | DB schema reference |
