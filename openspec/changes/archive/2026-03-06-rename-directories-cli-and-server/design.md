## Context

Two directories carry stale names: `tools/emergent-cli/` (pre-rename from when the binary was called `emergent`) and `apps/server-go/` (kept to distinguish from a now-deleted NestJS `apps/server/`). Both names are purely historical and add noise.

## Goals / Non-Goals

**Goals:**
- `tools/emergent-cli/` → `tools/cli/`, binary artifact `emergent-cli` → `memory`
- `apps/server-go/` → `apps/server/`
- All import paths, go.mod, workflows, scripts, Dockerfiles, and runtime path literals updated
- Clean `go build ./...` and `task cli:install` after changes

**Non-Goals:**
- No behaviour changes
- No API changes
- Old SDK module proxy tags (`apps/server-go/pkg/sdk/vX.Y.Z`) are not retagged — external consumers on old versions are unaffected

## Decisions

**Approach: bulk sed + targeted fixes**
Run `find | xargs sed` for import path replacement, then fix go.mod files, then fix runtime path literals manually (only 3 files). Same pattern used successfully in the previous rename.

**Binary artifact name: `emergent-cli` → `memory`**
The CLI command is already `memory`. Keeping the tarball named `emergent-cli` would be confusing. Renaming the artifact to `memory` makes install scripts and Homebrew formula consistent.

**SDK git tag format: `apps/server/pkg/sdk/vX.Y.Z`**
New releases use the new path. Old tags remain valid on the Go module proxy forever. No retag needed.

## Risks / Trade-offs

- [Runtime path literals] Three source files use `"apps/server-go"` as a literal string in `os.Stat`/path joins — silently wrong if missed → Mitigated by targeted grep after bulk rename
- [Workflow file rename] `emergent-cli.yml` → `memory-cli.yml` or `cli.yml`; `server-go.yml` → `server.yml` — need to check if any external integrations reference the workflow file names by name → Low risk, internal only
- [Local dev state] Any local `air` process or build cache referencing the old paths will break until rebuilt → Use `task stop && task start` after the rename; Air picks up the new paths automatically

## Migration Plan

1. Stop background server (Air hot-reload)
2. `git mv tools/emergent-cli tools/cli`
3. `git mv apps/server-go apps/server`
4. Bulk replace import paths in all `.go` files
5. Update all 7 `go.mod` files
6. Update runtime path literal strings in 3 Go source files
7. Update GitHub Actions workflow files
8. Update Taskfile.yml, shell scripts, Dockerfiles
9. `go mod tidy` in each module
10. `go build ./...` — must be clean
11. `task cli:install && memory version`
12. Commit
