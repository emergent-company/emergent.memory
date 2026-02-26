## Why

The monorepo uses Nx (JS) as a task runner and `tools/workspace-cli` (TypeScript/PM2) for process management — both are JS ecosystem overhead with no real benefit now that the backend is pure Go. `task` (Taskfile.dev) is already installed (`/usr/local/bin/task`) and `apps/server-go` already has a `Taskfile.yml` with test/build/migrate tasks. Completing the migration removes ~1GB of `node_modules`, the Nx build graph, and the PM2 process manager.

## What Changes

- **Create root `Taskfile.yml`**: includes `apps/server-go/Taskfile.yml` as a namespace and adds workspace-level tasks (dev, server start/stop/status, docker deps, swagger)
- **Expand `apps/server-go/Taskfile.yml`**: add missing tasks (swagger, dev with air, start-dev, docker-build) not yet covered
- **Remove `tools/workspace-cli/`**: TypeScript PM2 wrapper no longer needed
- **Remove `nx.json`** and **`apps/server-go/project.json`**: Nx descriptors gone
- **Slim `package.json`**: remove workspace-cli workspace entry, all nx devDeps, all dead LangChain/frontend deps; keep only what scripts/ or husky still need (or remove entirely)
- **Remove root `node_modules/`**: after slimming deps
- **Update docs**: AGENTS.md, openspec/project.md, .opencode/instructions.md — swap `nx run server-go:*` for `task *` and `pnpm workspace:*` for `task *`

## Capabilities

### New Capabilities

### Modified Capabilities
- `deployment`: Task runner changes from Nx+workspace-cli to Taskfile

## Impact

- `Taskfile.yml` (root) — new file
- `apps/server-go/Taskfile.yml` — expanded
- `tools/workspace-cli/` — removed
- `nx.json`, `apps/server-go/project.json` — removed
- `package.json` — slimmed or removed
- `node_modules/` — removed
- `AGENTS.md`, `openspec/project.md`, `.opencode/instructions.md` — updated commands
