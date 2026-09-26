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

  Authors do **not** merge their own PRs — a review is required. The merge is performed by a maintainer, the review bot (apparatus in `emergent-company/emergent.memory.infra`), or a **reviewer agent** (the general/orchestrator agent acting as *independent reviewer*, not as the PR author). A reviewer agent may merge only after its own review passes, CI is green, and no `CHANGES_REQUESTED` review is outstanding.
- **Cleanup** (after merge): `git worktree remove /root/emergent.memory-wt/<slug>` and delete the branch.
- **Exceptions:** read-only lanes (`@explorer`, `@oracle`, `@librarian`) and a single writer when no other session is active in the checkout.
- Full protocol, safety guards, and state tracking: load the `worktrees` skill.
- Before any build/commit: check `git status` + `git log --oneline -5`, and stage only files you authored — never sweep a parallel session's WIP into your commit.

## Feature Work — Spec + Implementation in One PR

Non-trivial work is spec-driven **and** ships as a **single** pull request. The OpenSpec change and its implementation are one unit of work — never two.

- **Small changes skip the spec.** Trivial work — a typo, a one-line fix, an isolated small edit with no behavior, interface, schema, or API change — goes straight to implementation.
- **Everything else starts with an OpenSpec change:** `openspec new change "<name>"` → artifacts in `openspec/changes/<name>/` (proposal, tasks, delta specs).
- **Implement in the same worktree and the same branch.** Do not create a second worktree or a second PR for the spec.
- **Open exactly one PR** containing both the change directory and the implementation (code, tests, and spec edits together). A spec-only PR that waits to merge before implementation begins is **not** the workflow.
- **Do not block on the spec.** Once the artifacts are written, keep going on the same branch; the change gets reviewed once, as part of the finished PR.
- **Archive after merge.** Run `openspec archive` (sync delta specs → `openspec/specs/`) as a post-merge follow-up, never as a pre-implementation PR.
- **PR description:** link the OpenSpec change directory and summarize the delta specs.

## Out-of-Scope Findings — File a GitHub Issue

While working, you'll notice things outside the current task: unrelated bugs, missing edge cases, refactors, feature ideas, or optional improvements. Do **not** fix them inline or let them silently disappear.

- If a finding is **not part of the current task's scope**, create a GitHub issue instead of expanding the PR.
- **Search first** to avoid duplicates: `gh issue list --repo emergent-company/emergent.memory --search "<keywords>"`. If a matching open issue exists, link to it rather than opening a new one.
- **Get user confirmation before creating** — show the drafted title and body and let the user edit, add context, or decline. Do not open the issue autonomously. (Searching and linking to an existing issue do not require confirmation.)
- **Create** with a clear title and body:

  ```bash
  gh issue create --repo emergent-company/emergent.memory \
    --title "<concise summary>" \
    --body "<what you found, where, why it matters, suggested fix if known>"
  ```

- If the finding relates to your current PR, reference it in the PR description or commit message (`Refs #<issue>`).
- Never include secrets, tokens, or credentials in the issue body.

## Commands

```bash
# Server (run from repo root)
task build          # build Go server binary
task test           # unit tests
task test:integration   # integration tests (apps/server/tests/integration)
task test:e2e       # API e2e suites (e2e/tests/api, runlog)
task lint           # all linters via lefthook (server, CLI, web UI, connector)
task hooks:install  # install git hooks via lefthook (once per checkout)
task migrate:up     # run Goose migrations
task migrate:status
task cli:install    # build + install memory CLI → ~/.memory/bin/memory

# Web UI (apps/web-ui) — Go templ + HTMX gateway
cd apps/web-ui
task dev        # gateway with air hot reload (templ + tailwind + go)
task lint       # web-ui + connector linters (repo-root lefthook config)
task fmt
task e2e:test   # Playwright e2e (see tests/e2e/README.md)

# CLI e2e (e2e/) — runlog-driven, requires Docker + runlog
task -d e2e test:mcj
```

> There is no server-local `apps/server/tests/e2e/` suite anymore. API e2e lives in `e2e/tests/api/`; CLI e2e lives in `e2e/tests/cli/`. The legacy `e2e/tests-api/` (NestJS-era testify module) was removed in favor of the consolidated runlog suite.

## Git Hooks — lefthook (single root config)

All git hooks live in the **one** repo-root `lefthook.yml`; install them with `task hooks:install` (or `lefthook install`).

- Git hooks are repo-global and lefthook loads exactly one config from the git root. There is **no** per-directory config (the old `apps/web-ui/lefthook.yml` and `.husky/` are gone).
- Each job is scoped with `root:` (its CWD) and `glob:` (patterns are matched **relative to `root`**), so server / CLI / web-ui / connector jobs coexist in one file.
- `pre-commit` is fast and path-scoped (gofmt, vet, build, lint-ratchet, Swagger `@Router`, migration SQL, untracked-import guard, ruff/templ). **Tests are not run on commit** — CI owns them.
- Secrets are scanned **repo-wide** by a single root `.gitleaks.toml`: strictly on staged changes at commit, whole-tree in the lint groups. Pre-existing findings are waived in `.gitleaksignore` — a **ratchet**: remove entries as you fix them, never add.
- Gateway Go jobs (`vet`/`build`/`test`/`golangci-lint`) need generated assets that aren't committed (templ output + compiled CSS). On an un-warmed tree they skip with instructions — run `task dev` (or `task generate && task css`) in `apps/web-ui` first; CI generates them before building.
- `lefthook run lint` = all trees (root `task lint`); `lefthook run lint-webui` = web-ui + connector (web-ui `task lint`).

## OpenSpec

Single OpenSpec root: `./openspec` (specs, changes, archive, config). Run all `openspec` commands from the repo root.

| Task | Command |
|---|---|
| List active changes | `openspec list` |
| List capability specs | `openspec list --specs` |
| Validate | `openspec validate` |

- **Placement**: a cross-app feature (e.g. backend + UI) is ONE change under `./openspec/changes/`; capability specs live at `./openspec/specs/<capability>/spec.md`.
- **Capability naming**: app-specific capabilities are prefixed — `web-*`, `ios-*`, `mac-*`, `cli-*`, `e2e-*`, `mcp-*`; unprefixed names only for genuinely cross-cutting capabilities.
- **Legacy spec-kit artifacts** (`specs/001-004`) were removed in favor of this single root — recoverable from git history.
- **Spec format**: main specs (`openspec/specs/<capability>/spec.md`) must use `## Purpose` + `## Requirements`; delta headers (`## ADDED/MODIFIED/REMOVED Requirements`) belong only inside `openspec/changes/<name>/specs/`.

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

### Dev host topology — one machine, several names

There is **one** dev machine. The names below all reach it; there is **no separate legacy
dev host** — never target one.

| Name / address | Reaches |
|---|---|
| `ssh emergent-dev` | hostname `emergent-dev` (the dev box) |
| `ssh memory-dev` | same machine — operator-provided; this alias is not defined on the orchestrator host |
| `10.10.10.40` | private IP of the same box |
| `100.117.62.45` | Tailscale IP of the same box |
| `46.4.253.156` / `api.dev.emergent-company.ai` | public address fronting the same box |
| `http://10.10.10.40:3002/health` | dev API (port `3002`) |

Deploys are **not scheduled** — trigger explicitly:

```bash
gh workflow run deploy-dev.yml --repo emergent-company/emergent.memory.infra --field target=both
```

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
