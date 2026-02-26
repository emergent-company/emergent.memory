## Context

`apps/admin` is a React/Vite/TypeScript SPA embedded in the Nx monorepo. It has two monorepo-relative dependencies that need fixing during extraction:

1. **Script reference**: `package.json` `precommit:validate` and `stories:validate` run `node ../../scripts/validate-story-duplicates.mjs` — relative to monorepo root
2. **Log path**: `vite.config.ts` constructs a log path `../../logs/admin` — relative to monorepo root
3. **Storybook nx commands**: `storybook` and `build-storybook` scripts call `nx run admin:serve:storybook` and `nx run admin:build:storybook`
4. **`project.json`**: Nx project descriptor — irrelevant outside the monorepo

The target `/root/emergent.memory.ui` is a fresh git repo with only a README.md.

## Goals / Non-Goals

**Goals:**
- Copy all admin source files into the root of `/root/emergent.memory.ui`
- Fix monorepo-relative paths so the app works standalone
- Copy `scripts/validate-story-duplicates.mjs` into `scripts/` in the new repo
- Remove `project.json` from the new repo
- Remove `apps/admin/` from the monorepo and update monorepo workspace config
- Update monorepo docs (AGENTS.md, openspec/project.md) to reflect the admin is in a separate repo

**Non-Goals:**
- Setting up CI/CD in the new repo
- Running `npm install` or building
- Changing any React component, hook, or page logic

## Decisions

**D1: Copy then remove (not git subtree/submodule)**
Use `rsync`/`cp` to copy files, then clean up the monorepo. Git history for the admin will start fresh in the new repo from whatever is in its current state. Avoids git subtree complexity for a one-time move.

**D2: Keep node_modules excluded**
Do not copy `node_modules/`, `dist/`, `playwright-report/`, or other generated artifacts. The new repo gets a clean install state.

**D3: Standalone Vite (no Nx)**
Remove `project.json` and fix Storybook scripts to use `storybook dev` / `storybook build` directly instead of `nx run admin:serve:storybook`. No Nx in the standalone repo.

**D4: Log path becomes local**
Change `../../logs/admin` → `./logs/admin` in `vite.config.ts` so logs write relative to the repo root.

**D5: Validate script becomes local**
Copy `scripts/validate-story-duplicates.mjs` and update `package.json` script paths from `../../scripts/` to `./scripts/`.

## Risks / Trade-offs

- **Risk**: Some other monorepo-relative import missed → Mitigation: grep for `../../` in vite.config.ts and package.json before considering done
- **Risk**: `.env` files with secrets inadvertently committed → Mitigation: confirm `.gitignore` covers `.env` (already exists in admin)
- **Risk**: Monorepo workspace config still references `apps/admin` after removal → Mitigation: explicit task to update `package.json` workspaces and `nx.json`

## Migration Plan

1. Copy `apps/admin/` contents (excluding node_modules, dist) to `/root/emergent.memory.ui/`
2. Fix `package.json`: script paths, remove Nx storybook commands
3. Fix `vite.config.ts`: log path
4. Copy `scripts/validate-story-duplicates.mjs` → `scripts/`
5. Remove `project.json`
6. Update monorepo: remove `apps/admin/` dir, remove from `package.json` workspaces, update `nx.json`
7. Update AGENTS.md and openspec/project.md

**Rollback**: The monorepo still has the old `apps/admin/` in git history. The new repo is additive.
