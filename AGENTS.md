# Memory

Monorepo for the Memory knowledge graph platform.

## Repository Layout

Apps live under `apps/`:

| App | Dir | Stack | Key commands |
|---|---|---|---|
| Server | `apps/server/` | Go · Echo · Bun ORM · fx · Zitadel | `task build` · `task test` · `task lint` · `task dev` |
| Web UI | `apps/web-ui/` | Go templ + HTMX gateway · Playwright e2e | `cd apps/web-ui && task dev` / `task lint` / `task e2e:test` |
| CLI | `apps/cli/` | Go | `task cli:install` (→ `~/.memory/bin/memory`) |
| Linux connector | `apps/connector.linux/` | Go | `cd apps/connector.linux && go build ./...` |
| Mac connector | `apps/connector.mac/` | Swift (`MemoryConnector/`) | build via Xcode |
| iOS app | `apps/ios/` | Swift | build via Xcode |

**Go workspace:** `go.work` at the repo root. Modules and their import paths:

| Dir | Module path |
|---|---|
| `apps/server` | `github.com/emergent-company/emergent.memory` |
| `apps/server/pkg/sdk` | `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk` |
| `apps/cli` | `github.com/emergent-company/emergent.memory/apps/cli` |
| `apps/connector.linux` | `github.com/emergent-company/emergent.memory/apps/connector.linux` |
| `apps/web-ui/gateway` | `github.com/emergent-company/emergent.memory/apps/web-ui` |
| `e2e` | `github.com/emergent-company/emergent.memory/e2e` |
| `blueprints/code-memory-blueprint/tools/*` | per-tool modules |

Since `apps/server` is the module root, non-SDK server packages import as `github.com/emergent-company/emergent.memory/pkg/…` (e.g. `…/pkg/apperror`) and `…/domain/…`. Note `pkg/sdk` is a **separate nested module**, so it imports via the longer path above.

**Server architecture:** Echo (HTTP) · Bun ORM (pgx/Postgres) · fx (dependency injection) · Zitadel (auth).

**Domain layout** (`apps/server/domain/<name>/` — 48 domains): agentcompat, agents, apitoken, authinfo, autoprovision, backups, blueprints, branches, chat, chunking, chunks, devtools, discoveryjobs, docs, documents, email, embeddingpolicies, events, extraction, graph, health, invites, journal, mcp, mcpregistry, mcprelay, modelconfig, monitoring, notifications, orgs, projects, provider, sandbox, sandboximages, scheduler, schemaregistry, schemas, search, sessiontodos, skills, standalone, superadmin, tasks, tracing, useraccess, useractivity, userprofile, users

Each domain: `handler.go` (Echo routes) · `service.go` (business logic) · `store.go` (Bun ORM queries) · `module.go` (fx wiring)

**DB:** Postgres on port `5432` by default (`POSTGRES_PORT`; dev compose maps `${POSTGRES_PORT:-5432}`). The e2e stack maps `5436` (see `e2e/docker-compose.yml`). Schemas: `kb` (knowledge), `core` (users/orgs). Migrations in `apps/server/migrations/` via Goose.

**CLI:** source at `apps/cli/` · install with `task cli:install` → `~/.memory/bin/memory` · defaults to `http://localhost:3012`; override with `--server <url>`

## Parallel Work — Worktrees (mandatory)

Never make code or doc changes directly in the shared checkout (`/root/emergent.memory`) while other sessions may be active — uncommitted work there mixes with other sessions' branches. All writer work happens in an isolated Git worktree.

- **Location:** `/root/emergent.memory-wt/<slug>` (this repo's existing convention — outside the repo, so no `.gitignore` entry is needed).
- **Branch:** `docs/<slug>`, `fix/<slug>`, or `feat/<slug>`, based on the default branch `main`.
- **Create:**

  ```bash
  cd /root/emergent.memory
  git worktree add -b <branch> /root/emergent.memory-wt/<slug> origin/main
  ```

- **Edit, build, test, and commit only inside the worktree.** The shared checkout is for orchestration and read-only recon.
- **Finish:** push the branch and open a PR against `main`:

  ```bash
  cd /root/emergent.memory-wt/<slug>
  git push -u origin <branch>
  gh pr create --base main --fill
  ```

  Authors do **not** merge their own PRs — the review bot (`emergent-code-reviewer`) reviews and merges once checks pass.
- **Cleanup** (after merge): `git worktree remove /root/emergent.memory-wt/<slug>` and delete the branch.
- **Exceptions:** read-only lanes (`@explorer`, `@oracle`, `@librarian`) and a single writer when no other session is active in the checkout.
- Full protocol, safety guards, and state tracking: load the `worktrees` skill.
- Before any build/commit: check `git status` + `git log --oneline -5`, and stage only files you authored — never sweep a parallel session's WIP into your commit.

## Commands

```bash
# Server (run from repo root)
task build          # build Go server binary
task test           # unit tests
task test:integration   # integration tests (apps/server/tests/integration)
task test:e2e       # API e2e suites (e2e/tests-api, separate Go module)
task lint           # Go linter
task migrate:up     # run Goose migrations
task migrate:status
task cli:install    # build + install memory CLI → ~/.memory/bin/memory

# Web UI (apps/web-ui) — Go templ + HTMX gateway
cd apps/web-ui
task dev        # gateway with air hot reload (templ + tailwind + go)
task lint       # lefthook: golangci-lint, go vet/test, templ, gitleaks
task fmt
task e2e:test   # Playwright e2e (see tests/e2e/README.md)

# CLI e2e (e2e/) — runlog-driven, requires Docker + runlog
task -d e2e test:mcj
```

> There is no server-local `apps/server/tests/e2e/` suite anymore. API e2e lives in `e2e/tests-api/`; CLI e2e lives in `e2e/`.

## Hot Reload — DO NOT restart after code changes

The Go server uses `air`. Changes are picked up in 1-2 seconds automatically.

- **Just save the file** — hot reload handles Go handler/service/store changes
- **Restart only for**: new fx modules in `cmd/server/main.go`, env var changes, after `go mod tidy`, server down
- **After structural refactors** (new packages, moved types): restart `air` explicitly — incremental reload may silently use stale binary
- **Confirm reload worked**: check `apps/server/logs/` for a fresh startup line before testing

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
| Go templ page/component | `apps/web-ui/gateway/AGENTS.md` — gateway Go/templ guidelines |
| E2E test (Playwright) | `apps/web-ui/tests/e2e/README.md` — projects, auth, helpers, test-id convention |
| CLI e2e test (runlog) | `e2e/AGENTS.md` |
| Go endpoint | `apps/server/AGENT.md` — fx modules, Echo, Bun ORM |
| Database entity | `apps/server/AGENT.md` — Bun models, kb/core schemas |

Common mistakes: hand-editing generated `*_templ.go` (run `templ generate`), skipping the gateway `go build`/`task lint`, and brittle e2e selectors (prefer `getByRole`/`name=` plus the gateway `data-testid` convention).

## Code Style

- **Go**: `gofmt`, no unused imports, wrap errors: `fmt.Errorf("context: %w", err)`
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
apps/server/logs/server/    # server logs
```

## Gotchas

- `docs/site/` is tracked in git — do NOT add to `.gitignore`
- **SSH timeouts**: SSH commands to remote servers time out at ~120s. For long operations (builds, test suites), run in background: `ssh root@your-server "nohup <cmd> > /tmp/out.log 2>&1 &"`. Use `gh run watch` to track CI instead of polling manually.
- **Ephemeral container deploys**: copying a binary into a running Docker container is temporary — a container restart reverts to the image version. All permanent deployments require a tagged release pushed through CI.
- **Legacy docs**: `docs/testing/AI_AGENT_GUIDE.md` targets the removed NestJS/nx stack — treat as historical; use `apps/server/AGENT.md` for testing guidance.

## Detail Docs

| File | Contents |
|------|----------|
| `apps/server/AGENT.md` | fx modules, Echo handlers, Bun ORM, job queues, testing |
| `apps/server/migrations/README.md` | Goose migration workflow |
| `apps/web-ui/gateway/AGENTS.md` | gateway Go/templ guidelines + verify commands |
| `apps/web-ui/tests/e2e/README.md` | Playwright e2e suite: projects, auth, coverage, test-id convention |
| `e2e/AGENTS.md` | CLI e2e suite (runlog), env vars, package layout |
| `docs/database/schema-context.md` | DB schema reference |
