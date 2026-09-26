## Why

The repo carries two competing git-hook systems and neither is actually active:

- `.husky/pre-commit` — a 275-line shell hook. It has no husky dependency (no `package.json`, no `.husky/_` shim, no `core.hooksPath`), and its paths are stale: it references `tools/emergent-cli` (now `apps/cli`) and the old module prefix `github.com/emergent-company/emergent/` (now `…/emergent.memory`).
- `lefthook.yml` at the repo root — server-focused (gofmt / vet / build / lint-ratchet).
- `apps/web-ui/lefthook.yml` — web-ui focused, but **dead for git hooks**: git hooks are repo-global, and lefthook loads exactly one config from the git root. It only ran because `task lint` `cd`'d into `apps/web-ui`. It also references a `connector/` directory that no longer exists.

Result: no pre-commit hook runs on any host, so the quality gates the hooks were meant to enforce (formatting, untracked-import guard, migration sanity, env-encoded framework rules) are silently absent, and the config that does exist is split and partly broken.

## What Changes

- **One config.** Consolidate into a single repo-root `lefthook.yml`. Scope every job with `root:` (its CWD) and `glob:` (matched relative to `root`) so server, CLI, web-ui, and connector jobs coexist in one file.
- **Port the husky checks** into lefthook pre-commit jobs: migration SQL validation, untracked-Go-import guard (fixed module prefix/paths), CLI golangci-lint (fixed path), repo gofmt, golangci config verify, and the per-staged-handler Swagger `@Router` check.
- **Keep pre-commit fast.** Heavy or CI-duplicative checks stay out of pre-commit: husky's scoped unit tests are dropped from pre-commit (CI owns tests); the full `lint` group carries vet/build/test/golangci for every tree.
- **Delete husky.** Remove `.husky/` and `apps/web-ui/lefthook.yml`.
- **Lint entry points.** Root `task lint` → `lefthook run lint` (all trees); `apps/web-ui` `task lint` → `lefthook run lint-webui` (web-ui + connector only, preserving its previous scope). Add `task hooks:install`.
- **Docs.** Update `AGENTS.md`, `CONTRIBUTING.md`, and the web-ui operations spec to describe the single-config model and the install step.

## Capabilities

### New Capabilities

- `git-hooks`: a single repo-root lefthook configuration that installs git hooks once and runs fast, path-scoped pre-commit checks plus a full lint group.

### Modified Capabilities

<!-- none -->

## Impact

- **Config:** rewrite `lefthook.yml` (repo root); delete `.husky/pre-commit` and `apps/web-ui/lefthook.yml`.
- **Taskfiles:** `Taskfile.yml` (`lint`, new `hooks:install`), `apps/web-ui/Taskfile.yml` (`lint` → `lint-webui`).
- **Docs:** `AGENTS.md`, `CONTRIBUTING.md`, `apps/web-ui/docs/spec/12-operations.md`.
- **Tooling dependency:** lefthook v2 (`github.com/evilmartians/lefthook/v2`); must be installed (`task hooks:install` / `lefthook install`).
- **Out of scope:** there is no root `.gitleaks.toml` (secrets scan stays scoped to `apps/web-ui/.gitleaks.toml`); a repo-wide secret scan is a follow-up.
