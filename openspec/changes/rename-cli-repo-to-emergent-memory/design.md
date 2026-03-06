## Context

The Go monorepo is currently named `emergent` on GitHub (`emergent-company/emergent`) and uses the Go module path `github.com/emergent-company/emergent`. Every other repo in the org already follows the `emergent.memory.*` pattern. This design covers how to execute the rename without breaking the build, CI, or existing deployments.

The rename touches three distinct layers:
1. **Go module system** — module declarations + import paths in source
2. **GitHub-hosted artifacts** — release download URLs, API endpoints, install scripts
3. **Developer tooling** — agent instruction files, local path references

There are no database changes, no API contract changes, and no product behavior changes.

## Goals / Non-Goals

**Goals:**
- Rename GitHub repo from `emergent-company/emergent` to `emergent-company/emergent.memory`
- Update Go module path from `github.com/emergent-company/emergent` to `github.com/emergent-company/emergent.memory` in all 7 `go.mod` files and 393 Go source files (826 occurrences)
- Fix all functional references to the old GitHub repo path (upgrade command, install scripts, CI ldflags, Homebrew formula)
- Update developer-facing docs and instruction files to reflect new paths
- Keep the build green and CI passing after the change

**Non-Goals:**
- Renaming Docker/GHCR image names (`ghcr.io/emergent-company/emergent-*`) — these are independent of the GitHub repo name
- Renaming the CLI binary (`emergent`) — this is a product name
- Migrating historical/archival docs in `openspec/changes/archive/`
- Supporting the old module path with redirect or alias

## Decisions

### Decision 1: Rename Go module path (not just the GitHub repo)

**Chosen:** Rename the Go module path alongside the GitHub repo rename.

**Rationale:** If only the GitHub repo is renamed, all Go import paths remain as `github.com/emergent-company/emergent` — which still resolves via GitHub's redirect, but creates a permanent inconsistency. Anyone reading the source would see a module path that doesn't match the repo name. The rename is the right time to fix both.

**Alternative considered:** Keep the Go module path as-is (only rename the GitHub repo). Rejected because it bakes in a permanent lie — the module path would forever refer to a repo that no longer exists under that name.

### Decision 2: Bulk `sed` replacement for Go source files

**Chosen:** Use `sed -i` (or `find | xargs sed`) for the 826 import-path occurrences across 393 `.go` files, followed by a full `go build ./...` to verify.

**Rationale:** This is a purely mechanical string substitution (`github.com/emergent-company/emergent` → `github.com/emergent-company/emergent.memory`). The old string does not appear as a substring of any other valid identifier, so the replacement is unambiguous. Manual editing 393 files is error-prone; automated replace + compile verification is safer.

**Alternative considered:** `gorename` or `gofmt`-based tooling. Rejected — those tools are for Go identifier renaming, not module path rewriting. `sed` on import strings is the standard approach for module renames.

### Decision 3: Dot in Go module path is acceptable

**Chosen:** Use `github.com/emergent-company/emergent.memory` as the new module path.

**Rationale:** Dots in Go module path segments are valid (e.g., `github.com/BurntSushi/toml`, `github.com/google/go-cmp`). The Go toolchain, `go get`, and the module proxy all handle them correctly. The `go.work` file uses relative `use` directives and contains no module path strings, so no change is needed there.

**Risk:** The Go module proxy has permanently cached `github.com/emergent-company/emergent` at released versions. These cached entries are immutable — they cannot be updated to the new path. Any external consumers who have pinned `github.com/emergent-company/emergent/apps/server-go/pkg/sdk@vX.Y.Z` will stop receiving updates until they migrate to the new module path. This is acceptable: the SDK is internal-first and external consumers are expected to track the latest.

### Decision 4: Single atomic commit for Go source changes

**Chosen:** All `go.mod` and `.go` source file changes land in one commit so the repo is never in a state where module declarations and import paths are mismatched.

**Rationale:** A partial rename (e.g., `go.mod` updated but source files not yet updated) breaks `go build`. Keeping it atomic prevents any broken intermediate state that could confuse CI or other developers.

### Decision 5: Update `go.sum` files after replacement

**Chosen:** Run `go mod tidy` in each workspace module after updating `go.mod` to regenerate `go.sum` with the new module path.

**Rationale:** `go.sum` entries are keyed by module path. After renaming, the old entries are stale and `go mod tidy` will produce the correct new entries. The workspace (`go.work`) `use` directives are path-relative and do not need updating.

### Decision 6: GitHub redirect covers transient period

**Chosen:** Rely on GitHub's automatic redirect from old repo URL to new URL for the transition period.

**Rationale:** GitHub automatically redirects `github.com/emergent-company/emergent` to `github.com/emergent-company/emergent.memory` after a rename. This means existing `git clone` URLs, `go get` commands using the old path, and CI pipelines that haven't been updated yet will continue to work temporarily. This gives a safe window to roll out the change without an instantaneous hard cutover.

**Caveat:** GitHub redirects are not permanent — they break if the old name is reused for a new repo. The redirect should not be relied on long-term.

## Risks / Trade-offs

| Risk | Mitigation |
|---|---|
| `go build` fails after replacement if any occurrence is missed | Run `go build ./...` across all workspace modules as the final verification step before committing |
| External SDK consumers on the old module path stop receiving updates | Accepted. SDK is internal-first. Document the breaking change in the release notes. |
| CI pipelines that hardcode the repo name break between GitHub rename and code push | The ldflags in `emergent-cli.yml` reference the module path, which must be updated in the same PR as the source rename |
| GitHub redirect breaks if `emergent` repo name is reused | Don't reuse the name. No action needed. |
| Homebrew tap breaks until `emergent-cli.rb` is updated | Update the formula in the same PR; the formula uses versioned release URLs that must match the new repo name |
| `emergent upgrade` command fetches from wrong repo until updated | The GitHub API URL in `upgrade.go:372` must be updated in the same commit |
| `go.sum` entries are stale after `go.mod` rename | Run `go mod tidy` per module; verify with `go mod verify` |

## Migration Plan

The rename executes in a single coordinated step — there is no multi-phase rollout because the Go module system does not support path aliases.

**Order of operations:**

1. **Rename GitHub repository** (UI action: Settings → Rename)
   - GitHub redirect activates immediately for old URLs

2. **Update Go module files** (in one commit)
   - `go.mod`: update `module` declarations in all 7 files
   - `go.mod`: update `replace` directives and `require` entries referencing the old path
   - Run bulk `sed` on all `.go` files: `github.com/emergent-company/emergent` → `github.com/emergent-company/emergent.memory`
   - Run `go mod tidy` in each module
   - Run `go build ./...` to verify zero compilation errors

3. **Update functional GitHub URL references** (same commit or immediate follow-up)
   - `tools/emergent-cli/internal/cmd/upgrade.go`: GitHub API URL
   - `install.sh`, `tools/emergent-cli/install.sh`: `GITHUB_REPO` variable
   - `.github/workflows/emergent-cli.yml`: `-ldflags` module paths
   - `deploy/homebrew/emergent-cli.rb`: release download URLs
   - `deploy/minimal/` scripts: clone and raw content URLs

4. **Update docs and developer tooling** (same PR)
   - `mkdocs.yml`: `repo_url`, `repo_name`
   - `AGENTS.md`, `/root/emergent.memory.ui/AGENTS.md`
   - Skill files referencing `/root/emergent`

5. **Update local git remote**
   ```bash
   git remote set-url origin https://github.com/emergent-company/emergent.memory.git
   ```

6. **Tag and release** under new repo name to populate new GitHub release URLs referenced by Homebrew

**Rollback:** Not applicable for a rename — reverting means renaming back on GitHub and reverting the commits. GitHub allows renaming back, and the old redirect would reactivate.

## Open Questions

- Should the Homebrew tap (`deploy/homebrew/`) be moved to a separate `emergent.memory.tap` repo to match the naming convention? Out of scope for this change but worth a follow-up.
- Should the `tests/api` module (`module github.com/emergent/api-tests`) be renamed too? Currently it uses a different path (`github.com/emergent/api-tests`, not `github.com/emergent-company/emergent`) so it is unaffected by this rename.
