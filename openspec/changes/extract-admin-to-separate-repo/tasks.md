## 1. Copy admin files to new repo

- [x] 1.1 Copy all files from `apps/admin/` to `/root/emergent.memory.ui/` excluding `node_modules/`, `dist/`, `playwright-report/`, `.tw-patch`, `adk_cov.out` and other generated artifacts (use rsync with excludes)
- [x] 1.2 Copy `scripts/validate-story-duplicates.mjs` into `/root/emergent.memory.ui/scripts/validate-story-duplicates.mjs`

## 2. Fix monorepo-relative paths in new repo

- [x] 2.1 Update `package.json` `precommit:validate` and `stories:validate` scripts: `../../scripts/validate-story-duplicates.mjs` → `./scripts/validate-story-duplicates.mjs`
- [x] 2.2 Update `package.json` `storybook` script: remove `nx run admin:serve:storybook` call → use `storybook dev -p 6006` directly
- [x] 2.3 Update `package.json` `build-storybook` script: remove `nx run admin:build:storybook` call → use `storybook build` directly
- [x] 2.4 Update `vite.config.ts`: change log path `../../logs/admin` → `./logs/admin`
- [x] 2.5 Remove `project.json` from `/root/emergent.memory.ui/` (Nx-specific, not needed standalone)
- [x] 2.6 Grep for any remaining `../../` references in `/root/emergent.memory.ui/` and fix them

## 3. Remove admin from monorepo

- [x] 3.1 Delete `apps/admin/` directory from `/root/emergent`
- [x] 3.2 Update `/root/emergent/package.json` workspaces: remove `"apps/admin"` entry
- [x] 3.3 Update `/root/emergent/nx.json` `defaultBase`: change `"master"` → `"main"` (bonus fix while touching this file)

## 4. Update monorepo documentation

- [x] 4.1 Update `AGENTS.md`: replace admin component/hook table row with note pointing to `emergent.memory.ui` repo; update frontend section
- [x] 4.2 Update `openspec/project.md`: update Monorepo Structure section — remove `apps/admin` entry, add note that frontend is in separate repo (`emergent.memory.ui`)
- [x] 4.3 Update `openspec/project.md`: update Frontend section in Tech Stack to note the admin lives in `emergent.memory.ui`
- [x] 4.4 Update `.opencode/instructions.md`: update AGENT.md reference table — remove admin entry or note new repo

## 5. Validate

- [x] 5.1 Verify `/root/emergent.memory.ui/` contains `src/`, `package.json`, `vite.config.ts`, `index.html`
- [x] 5.2 Verify no `../../` paths remain in `/root/emergent.memory.ui/package.json` and `vite.config.ts`
- [x] 5.3 Verify `apps/admin/` no longer exists in `/root/emergent`
- [x] 5.4 Verify `package.json` workspaces in `/root/emergent` no longer includes `apps/admin`
