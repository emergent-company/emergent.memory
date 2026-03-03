---
name: pre-commit-check
description: Run the pre-commit validation checks for the emergent Go monorepo. Use before committing to verify Go builds, unit tests, Swagger annotations, SDK tests, docs build, and linting all pass.
license: MIT
metadata:
  author: emergent
  version: "1.0"
---

# Skill: pre-commit-check (emergent monorepo)

Run the appropriate subset of checks based on what files were changed.
All commands run from `/root/emergent` (the repo root) unless noted.

---

## Decision tree — which checks to run

### Always run

```bash
# Confirm the repo root
pwd  # should be /root/emergent or navigate there
```

### If any `apps/server-go/**/*.go` files changed

```bash
# 1. Build
go build ./...
# run from apps/server-go/
```

```bash
# 2. Unit tests (fast — excludes e2e)
task test
# run from apps/server-go/ — equivalent to: go test ./... -v -count=1
# (excludes ./tests/e2e/... which require a live server)
```

```bash
# 3. Lint
task lint
# run from apps/server-go/ — runs golangci-lint run ./...
# Only run if the change touches non-trivial logic (not just docs/comments/config)
```

### If any `apps/server-go/domain/*/handler.go` files changed (Swagger hook)

The Git pre-commit hook at `.git/hooks/pre-commit` runs automatically on
`git commit` — it checks that every staged `handler.go` has at least one
`// @Router` annotation. To simulate it manually before staging:

```bash
# Check staged handler files for @Router annotations
git diff --cached --name-only --diff-filter=ACMR \
  | grep '^apps/server-go/domain/.*/handler\.go$' \
  | xargs -I{} grep -l "^// @Router" {}
```

If any handler file is missing `// @Router`, add the annotation block before
committing. See `apps/server-go/domain/chunks/handler.go` for examples.

### If any `apps/server-go/pkg/sdk/**/*.go` files changed

```bash
# SDK tests (separate module)
go test ./...
# run from apps/server-go/pkg/sdk/
```

### If any `docs/**` or `mkdocs.yml` files changed

```bash
mkdocs build --strict
# run from repo root — requires mkdocs to be installed
```

### If any `.github/workflows/*.yml` files changed

```bash
python3 -c "import yaml; yaml.safe_load(open('<file>'))" 
# for each changed workflow file — validates YAML syntax
```

---

## Quick reference — working directories

| Check | Working directory |
|---|---|
| `go build ./...` | `apps/server-go/` |
| `task test` | `apps/server-go/` |
| `task lint` | `apps/server-go/` |
| `task build` | `apps/server-go/` |
| SDK `go test ./...` | `apps/server-go/pkg/sdk/` |
| `mkdocs build --strict` | repo root (`/root/emergent`) |

---

## Failure handling

1. **Build fails** — fix compilation errors before committing; do not proceed
2. **Unit tests fail** — fix the broken tests; do not commit failing tests
3. **Swagger hook fails** — add missing `// @Router` annotations to the handler
4. **Lint fails** — fix lint errors; if a lint rule is a false positive, consult
   `.golangci.yml` for suppression options rather than using `--no-verify`
5. **mkdocs fails** — fix broken links, missing references, or invalid YAML
6. **Never use `git commit --no-verify`** unless the user explicitly requests it

---

## Guardrails

- Run only the checks relevant to what changed — don't run the full e2e suite
  (`task test:e2e`) as part of a pre-commit check; it requires a live server
- If `task` is not installed, run the equivalent `go` commands directly
- SDK is a separate Go module — `go test ./...` from `apps/server-go/pkg/sdk/`
  is independent of `go test ./...` from `apps/server-go/`
- If unsure which checks apply, run build + unit tests — they are always safe
