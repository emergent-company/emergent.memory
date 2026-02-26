## Why

The React admin frontend (`apps/admin`) currently lives inside the Nx monorepo alongside the Go backend. Moving it to its own repository (`emergent.memory.ui`) enables independent deployment, versioning, and development without requiring the full monorepo checkout. The target repository already exists at `/root/emergent.memory.ui` (remote: `git@github.com:emergent-company/emergent.memory.ui.git`).

## What Changes

- **Copy** all files from `apps/admin/` into `/root/emergent.memory.ui/` (making it the repo root)
- **Detach from monorepo**: remove `project.json` (Nx-specific), update paths that reference `../../` (scripts, logs)
- **Make standalone**: copy shared script (`validate-story-duplicates.mjs`), update package.json script paths, fix Storybook scripts to not use `nx run`
- **Fix vite.config.ts**: update log path from `../../logs/admin` to `./logs/admin`
- **Remove from monorepo**: delete `apps/admin/` from `/root/emergent`, update `nx.json` and `package.json` workspace config
- **Update monorepo docs**: AGENTS.md, openspec/project.md — remove admin references or point to new repo

## Capabilities

### New Capabilities

### Modified Capabilities
- `deployment`: Admin frontend now lives in a separate repo with its own CI/CD lifecycle

## Impact

- `/root/emergent.memory.ui/` — receives all admin source files
- `/root/emergent/apps/admin/` — removed from monorepo
- `/root/emergent/nx.json`, `package.json`, `workspace.json` — updated to remove admin project
- `/root/emergent/AGENTS.md`, `openspec/project.md` — updated to reflect new repo location
