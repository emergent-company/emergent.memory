## Why

The directories `tools/emergent-cli/` and `apps/server-go/` contain legacy names from before the project was renamed to Memory. `emergent-cli` is redundant now that the binary is called `memory`, and `server-go` only existed to distinguish from the now-deleted NestJS `apps/server/`. Both names should be cleaned up for consistency.

## What Changes

- **BREAKING** `tools/emergent-cli/` → `tools/cli/` (directory rename)
- **BREAKING** Released binary/artifact name `emergent-cli` → `memory` (install scripts, Dockerfiles, GitHub release assets)
- **BREAKING** Go module path `github.com/emergent-company/emergent.memory/tools/emergent-cli` → `.../tools/cli`
- **BREAKING** `apps/server-go/` → `apps/server/` (directory rename)
- **BREAKING** Go module path `github.com/emergent-company/emergent.memory/apps/server-go/pkg/sdk` → `.../apps/server/pkg/sdk`
- **BREAKING** Git tag naming convention for SDK releases: `apps/server-go/pkg/sdk/vX.Y.Z` → `apps/server/pkg/sdk/vX.Y.Z`
- Update all Go import paths, go.mod files, GitHub Actions workflows, Taskfiles, shell scripts, Dockerfiles, and runtime path literals accordingly

## Capabilities

### New Capabilities
<!-- None — this is a pure rename with no behavioural changes -->

### Modified Capabilities
<!-- No spec-level requirement changes -->

## Impact

- **Go modules**: 7 go.mod files updated; all `require` and `replace` directives updated; `go mod tidy` needed in each
- **Go source**: 31 import-path occurrences in CLI module; 188 import-path occurrences across 76 files for server module
- **Runtime path literals**: 3 Go source files in `apps/server-go/` use the literal string `"apps/server-go"` in `os.Stat` / path joins — must be updated to `"apps/server"` or will silently fail at runtime
- **CI/CD**: `emergent-cli.yml`, `server-go.yml`, `server-go-sdk.yml` workflows; release tag logic in `scripts/release.sh`
- **Dockerfiles**: 3 files for CLI, 4 for server (including firecracker)
- **Install scripts**: `install.sh`, `deploy/minimal/install-online.sh`, various `deploy/minimal/` scripts
- **Docs/AGENTS.md**: ~50 markdown files across docs, skills, openspec — informational only (low risk)
- **External SDK consumers**: Old `apps/server-go/pkg/sdk` module proxy tags remain valid for old versions; new releases will tag under `apps/server/pkg/sdk/`
