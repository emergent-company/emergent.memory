## 1. Consolidate config

- [x] 1.1 Rewrite the repo-root `lefthook.yml` as the single config: `pre-commit` (fast, parallel) and `lint` (full) groups, every job scoped by `root:` + `glob:`; verified `lefthook validate` → "All good" and `lefthook run pre-commit` executes against staged files
- [x] 1.2 Port the husky server checks into `pre-commit` jobs: gofmt, `go vet`, `go build`, `lint-ratchet`, staged-handler Swagger `@Router` check, golangci config verify; verified gofmt blocks an unformatted staged file, swagger blocks a handler without `@Router`, and jobs with no matching staged file are skipped
- [x] 1.3 Port husky's untracked-Go-import guard with the corrected module prefix `github.com/emergent-company/emergent.memory` and `apps/`-rooted path resolution; verified it fails for a staged import of an untracked package and passes otherwise
- [x] 1.4 Port husky's migration SQL validation (backslash metacommands, spaced `$$`) as a `apps/server/migrations/*.sql` job; verified it fails on a seeded bad migration and passes on a clean one
- [x] 1.5 Move the web-ui jobs into the root config (ruff, gateway gofmt/vet/build, `templ generate -check`, gitleaks, no-generated guard) and add CLI (gofmt/golangci) and connector (gofmt/vet) jobs; fix the dead `connector/` root to `apps/connector.linux`; verified `webui-no-generated` blocks a staged `app.css` and `cli-gofmt` blocks an unformatted CLI file
- [x] 1.6 Add the full `lint` group (gofmt + vet + build + golangci for all four trees; go test for web UI + connector; ruff/templ/gitleaks for web UI) and a `lint-webui` group (web-ui + connector only)

## 2. Remove husky + dead config

- [x] 2.1 Delete `.husky/` (pre-commit) and `apps/web-ui/lefthook.yml`; `git status` shows only the intended deletions and no remaining references outside the OpenSpec change docs

## 3. Lint entry points

- [x] 3.1 Root `Taskfile.yml`: `lint` → `lefthook run lint`; added `hooks:install` → `lefthook install`
- [x] 3.2 `apps/web-ui/Taskfile.yml`: `lint` → `lefthook run lint-webui` (preserve web-ui + connector scope)
- [x] 3.3 `e2e/Taskfile.yml`: keep `lefthook run lint` (now resolves the root config)

## 4. Docs + spec

- [x] 4.1 `AGENTS.md`: fixed the `task lint` description, added `task hooks:install`, and added a "Git Hooks — lefthook" section
- [x] 4.2 `CONTRIBUTING.md`: added `lefthook` to prerequisites and an "Install git hooks" setup step
- [x] 4.3 `apps/web-ui/docs/spec/12-operations.md`: updated the "Linting & hooks" section to the single-root-config model

## 5. Verification

- [x] 5.1 `lefthook install` writes `.git/hooks/pre-commit` and a commit runs the hook — verified in a scratch repo (not installed into the shared checkout, to avoid mutating other sessions' hooks)
- [x] 5.2 `lefthook validate` → "All good"; targeted jobs in the `lint` group verified runnable (`webui-ruff` / `webui-secrets` skip cleanly when the tool is absent)
- [x] 5.3 `task -n lint` → `lefthook run lint`; `cd apps/web-ui && task -n lint` → `lefthook run lint-webui`
- [x] 5.4 `openspec validate migrate-husky-to-lefthook --strict` → "Change 'migrate-husky-to-lefthook' is valid"
- [x] 5.5 Staged deliberately unformatted `apps/server/**/*.go` → `server-gofmt` job blocked; probe reverted

## 6. Repo-wide secret scanning

- [x] 6.1 Add repo-root `.gitleaks.toml` (default ruleset + `memt_` rule + placeholder/env/vendor/test-fixture allowlists); delete `apps/web-ui/.gitleaks.toml`
- [x] 6.2 `pre-commit` job scans staged changes repo-wide with `gitleaks git --staged`; verified a staged fake key fails the job
- [x] 6.3 `lint` / `lint-webui` jobs scan the whole tree with `gitleaks dir .`; added `.gitleaksignore` ratchet for the 73 pre-existing findings; verified the scan is green (and that an unlisted leak still fails)
- [x] 6.4 `apps/web-ui` `secrets:scan` task now scans from the repo root with the root config

## 7. Follow-ups

- [ ] 7.1 Security: rotate/remove the pre-existing findings that look like real credentials (a DeepSeek-style key and `emt_` API tokens, among placeholders) — tracked in #1042
- [ ] 7.2 Optionally add a gitleaks step to CI so the gate also runs server-side

## 8. Review fixes (Copilot)

- [x] 8.1 Isolate `golangci-lint` caches per tree (`GOLANGCI_LINT_CACHE`) so parallel `lint` / `lint-webui` jobs cannot collide on the shared lock
- [x] 8.2 Guard gateway Go jobs (`vet`/`build`/`test`/`golangci`) on generated assets (templ output + `webui/static/css/app.css`): skip with instructions on an un-warmed tree instead of failing; added `webui-go-build` to both lint groups
- [x] 8.3 Add the missing module `go build` jobs (`cli-go-build`, `webui-go-build`, `connector-go-build`) so the `lint` group matches its spec
- [x] 8.4 Repo-wide `secrets` job (no web-ui glob) is intentional and documented — the earlier path-scoped `webui-secrets` finding is obsolete
