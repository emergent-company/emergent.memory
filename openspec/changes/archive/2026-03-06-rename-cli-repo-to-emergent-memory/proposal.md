## Why

All other repositories in the `emergent-company` GitHub org follow the `emergent.memory.*` naming convention (`emergent.memory.ui`, `emergent.memory.infra`, `emergent.memory.mac`), but the Go monorepo is still named `emergent` — making it the inconsistent outlier. Aligning the repo name closes that gap and makes the org's repo structure self-evident.

## What Changes

- **BREAKING** GitHub repository renamed: `emergent-company/emergent` → `emergent-company/emergent.memory`
- **BREAKING** Go module path renamed: `github.com/emergent-company/emergent` → `github.com/emergent-company/emergent.memory` across all `go.mod` files and ~130 Go source files
- Install scripts updated: `GITHUB_REPO` variable in `install.sh` and `tools/emergent-cli/install.sh`
- CLI upgrade command updated: GitHub API release URL in `tools/emergent-cli/internal/cmd/upgrade.go`
- CI workflow updated: `-ldflags` module path references in `.github/workflows/emergent-cli.yml`
- Homebrew formula updated: download URLs in `deploy/homebrew/emergent-cli.rb`
- Deployment scripts updated: `raw.githubusercontent.com` and `git clone` URLs across `deploy/minimal/`
- `mkdocs.yml` updated: `repo_url` and `repo_name` fields
- Agent/AI instruction files updated: `AGENTS.md`, cross-repo `emergent.memory.ui/AGENTS.md`
- Docker image names (`ghcr.io/emergent-company/emergent-*`) are **not** renamed — GHCR packages are independent of the GitHub repo name and will be handled separately if needed
- CLI binary name (`emergent`) is **not** renamed — it is a product name, not derived from the repo

## Capabilities

### New Capabilities

_None. This is a rename/refactor with no new product capabilities._

### Modified Capabilities

_None. No spec-level behavior changes — all existing capabilities continue to function identically under the new module path._

## Impact

**Go compilation:**
- 7 `go.mod` files declare `github.com/emergent-company/emergent` and must be updated
- ~130 `.go` source files contain ~900 import path occurrences requiring bulk replacement
- `go.work` workspace file uses relative paths — no module name references, no change needed

**CI/CD:**
- `.github/workflows/emergent-cli.yml`: 2 `-ldflags` paths + 1 `go get` install snippet
- Other workflow files use `${{ github.repository }}` dynamically and are unaffected

**CLI self-update:**
- `tools/emergent-cli/internal/cmd/upgrade.go:372`: hits `api.github.com/repos/emergent-company/emergent/releases/latest` — must change to new repo name or upgrade will silently target the old repo

**Install/deploy scripts:**
- `install.sh`, `tools/emergent-cli/install.sh`: `GITHUB_REPO` variable
- `deploy/minimal/install.sh`, `deploy/minimal/uninstall.sh`, `deploy/minimal/emergent-ctl.sh`: `githubusercontent.com` URLs
- `deploy/homebrew/emergent-cli.rb`: 4 versioned release download URLs

**Docs:**
- `mkdocs.yml`: `repo_url` + `repo_name`
- `apps/server-go/pkg/sdk/README.md`, `docs/llms.md`, `docs/llms-go-sdk.md`: public-facing `go get` instructions

**Developer tooling:**
- `AGENTS.md`: module name, local path `/root/emergent`
- `/root/emergent.memory.ui/AGENTS.md`: cross-repo reference `Backend lives at /root/emergent`
- `.opencode/skills/`, `.claude/skills/`, `.agents/skills/`: several skill files reference `/root/emergent`

**External consumers of the Go SDK:**
- Anyone pinning `github.com/emergent-company/emergent/apps/server-go/pkg/sdk@vX.Y.Z` must update their import paths. The old module path will remain cached on the Go module proxy but no new versions will be published under it.
