## Requirements

### Requirement: CLI module path uses tools/cli
The Go module for the CLI tool SHALL be located at `tools/cli/` and its module path SHALL be `github.com/emergent-company/emergent.memory/tools/cli`.

#### Scenario: CLI builds from new path
- **WHEN** `task cli:install` is run from repo root
- **THEN** binary is built from `tools/cli/` and installed as `~/.local/bin/memory`

### Requirement: Server module path uses apps/server
The Go backend module SHALL be located at `apps/server/` and its SDK module path SHALL be `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk`.

#### Scenario: Server builds from new path
- **WHEN** `go build ./...` is run from `apps/server/`
- **THEN** build completes without errors

### Requirement: Released binary is named memory
The CLI release artifact SHALL be named `memory` (not `emergent-cli`) in install scripts, Dockerfiles, and GitHub release assets.

#### Scenario: Install script installs memory binary
- **WHEN** `install.sh` is executed
- **THEN** the installed binary is named `memory` in the target directory
