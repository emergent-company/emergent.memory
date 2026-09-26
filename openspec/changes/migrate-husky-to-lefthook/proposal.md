## Why

The repo carries two competing git-hook systems and neither is actually active:

- `.husky/pre-commit` — a 275-line shell hook. It has no husky dependency (no `package.json`, no `.husky/_` shim, no `core.hooksPath`), and its paths are stale: it references `tools/emergent-cli` (now `apps/cli`) and the old module prefix `github.com/emergent-company/emergent/` (now `…/emergent.memory`).
- `lefthook.yml` at the repo root — server-focused (gofmt / vet / build / lint-ratchet).
- `apps/web-ui/lefthook.yml` — web-ui focused, but **dead for git hooks**: git hooks are repo-global, and lefthook loads exactly one config from the git root. It only ran because `task lint` `cd`'d into `apps/web-ui`. It also references a `connector/` directory that no longer exists.

Result: no pre-commit hook runs on any host, so the quality gates the hooks were meant to enforce (formatting, untracked-import guard, migration sanity, env-encoded framework rules) are silently absent, and the config that does exist is split and partly broken.

## What Changes

- **One config.** Consolidate into a single repo-root `lefthook.yml`. Scope every job with `root:` (its CWD) and `glob:` (matched relative to `root`) so server, CLI, web-ui, and connector jobs coexist in one file.
- **Port the husky checks** into lefthook jobs: migration SQL validation, untracked-Go-import guard (fixed module prefix/paths), CLI golangci-lint (fixed path), repo gofmt, golangci config verify, and the per-staged-handler Swagger `@Router` check. The CLI `golangci-lint` runs in the `lint` group (whole-module, `--new-from-rev HEAD`) rather than `pre-commit`: whole-module golangci-lint is too slow for the fast commit path and `apps/cli` carries pre-existing findings.
- **Keep pre-commit fast.** Heavy or CI-duplicative checks stay out of pre-commit: husky's scoped unit tests are dropped from pre-commit (CI owns tests); the full `lint` group carries vet/build/test/golangci for every tree.
- **Delete husky.** Remove `.husky/` and `apps/web-ui/lefthook.yml`.
- **Lint entry points.** Root `task lint` → `lefthook run lint` (all trees); `apps/web-ui` `task lint` → `lefthook run lint-webui` (web-ui + connector only, preserving its previous scope). Add `task hooks:install`.
- **Repo-wide secret scanning.** Replace the web-ui-scoped `.gitleaks.toml` with a single root `.gitleaks.toml`; run gitleaks **repo-wide** — strict on staged changes in `pre-commit`, and whole-tree in the lint groups. Pre-existing findings are waived by a committed `.gitleaksignore` **ratchet** (entries may only be removed, never added), so the full-tree scan starts green and only new leaks fail.
- **Docs.** Update `AGENTS.md`, `CONTRIBUTING.md`, and the web-ui operations spec to describe the single-config model and the install step.

## Capabilities

### New Capabilities

- `git-hooks`: a single repo-root lefthook configuration that installs git hooks once and runs fast, path-scoped pre-commit checks plus a full lint group.

### Modified Capabilities

<!-- none -->

## Impact

- **Config:** rewrite `lefthook.yml` (repo root); add root `.gitleaks.toml` + `.gitleaksignore`; delete `.husky/pre-commit`, `apps/web-ui/lefthook.yml`, and `apps/web-ui/.gitleaks.toml`.
- **Taskfiles:** `Taskfile.yml` (`lint`, new `hooks:install`), `apps/web-ui/Taskfile.yml` (`lint` → `lint-webui`, repo-wide `secrets:scan`).
- **Docs:** `AGENTS.md`, `CONTRIBUTING.md`, `apps/web-ui/docs/spec/12-operations.md`.
- **Tooling dependency:** lefthook v2 (`github.com/evilmartians/lefthook/v2`) and gitleaks v8; both must be installed (`task hooks:install` / `lefthook install`).
- **Security follow-up:** the first whole-tree scan surfaced pre-existing findings that look like **real** committed credentials (a DeepSeek-style key and `emt_` API tokens, among placeholders). They are waived in `.gitleaksignore` only to keep the ratchet green and must be rotated and removed under a separate security issue.
