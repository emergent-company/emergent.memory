## Context

Current state:
- `nx run server-go:<target>` — thin wrappers around shell commands defined in `apps/server-go/project.json`
- `pnpm workspace:*` → `tools/workspace-cli/src/cli.ts` → PM2 → runs `air` for the Go server
- `apps/server-go/Taskfile.yml` already exists with test/build/lint/migrate tasks
- `task` binary is at `/usr/local/bin/task`
- Dependency processes (Postgres, Zitadel) are managed externally — workspace-cli has an empty dependencies list
- The admin process config in workspace-cli references `apps/admin` (now removed)

## Goals / Non-Goals

**Goals:**
- Root `Taskfile.yml` at `/root/emergent/Taskfile.yml` with all workspace-level commands
- Expand `apps/server-go/Taskfile.yml` with missing targets (swagger, dev/air, start-dev)
- Remove workspace-cli, nx.json, project.json
- Remove root node_modules (and package.json or reduce to bare minimum for husky)
- Update all docs to reference `task` commands

**Non-Goals:**
- Replacing PM2 with a full process supervisor — `task dev` runs `air` in foreground; no daemonization
- Migrating the `scripts/*.ts` or MCP tools (separate concern)
- Changing the `apps/server-go/cmd/tasks` internal Go CLI (stays as-is)

## Decisions

**D1: Root Taskfile uses `includes` to delegate to server-go**
`Taskfile.yml` at root uses `includes: {server: {taskfile: apps/server-go/Taskfile.yml}}` so tasks are available as `task server:build`, `task server:test:e2e` etc. Also adds top-level aliases (`task build`, `task test:e2e`) for convenience.

**D2: Replace workspace:start/stop/status with simple task commands**
- `task dev` → runs `air` directly in `apps/server-go` (foreground, hot reload)
- `task start` → builds + runs server binary in background, writes PID to `pids/server.pid`
- `task stop` → kills PID from `pids/server.pid`
- `task status` → checks if PID is alive + hits `/health`
No PM2 dependency. Simple and transparent.

**D3: package.json — keep minimal for husky only**
Husky is the only non-replaceable JS tool (pre-commit hooks). Keep `package.json` with just husky + `prepare` script. Remove all other deps. Keep `node_modules` minimal (just husky).

**D4: Keep tools/*-mcp as-is**
The MCP tools have their own `package.json` files and `node_modules`. They are separate from the root workspace. Not touched in this change.

## Risks / Trade-offs

- **Risk**: CI/CD pipelines reference `nx run` → Mitigation: search `.github/` for nx references and update
- **Risk**: `apps/server-go/AGENT.md` mentions `nx run server-go:test-e2e` → Mitigation: update in doc tasks
- **Risk**: `husky` still needs minimal node_modules → Mitigation: `npm install` with just husky (~1MB vs 1GB)

## Migration Plan

1. Expand `apps/server-go/Taskfile.yml` with swagger, dev, start-dev targets
2. Create root `Taskfile.yml` with includes + workspace tasks
3. Remove `tools/workspace-cli/`
4. Remove `nx.json`, `apps/server-go/project.json`
5. Slim `package.json` to husky-only, run `npm install`
6. Update CI/CD if needed
7. Update AGENTS.md, openspec/project.md, .opencode/instructions.md
