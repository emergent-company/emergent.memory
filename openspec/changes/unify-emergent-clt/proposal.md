## Why

**There are TWO separate tools with duplicate functionality for controlling the standalone Docker installation:**

1. **`emergent-ctl`** (bash script) - Installed to `~/.emergent/bin/emergent-ctl` by the installer script
2. **`emergent-cli ctl`** (Go subcommand) - Part of the `emergent-cli` binary

Both provide identical commands: `start`, `stop`, `restart`, `status`, `logs`, `health`, `shell`. This duplication is confusing:

- Users don't know which one to use
- Documentation refers to both inconsistently
- Maintenance burden: changes must be made in two places
- The bash script (`emergent-ctl.sh`) wraps `docker compose`, while the Go code (`tools/emergent-cli/internal/cmd/ctl.go`) also wraps `docker compose` — identical logic in two languages

The installer even has `emergent-ctl.sh` call `docker exec emergent-server emergent` (line 111), which means the bash script is calling into the Go CLI for some commands — yet they both exist independently.

## What Changes

**Eliminate `emergent-ctl.sh` bash script and use only `emergent-cli ctl`**:

- **Remove `deploy/minimal/emergent-ctl.sh`** - Delete the bash script entirely
- **Update installer** (`deploy/minimal/install-online.sh`) - Remove bash script download, don't create `emergent-ctl` wrapper
- **Update `emergent-auth.sh`** - Change references from `emergent-ctl` to `emergent-cli ctl`
- **Keep `emergent-cli ctl` unchanged** - The Go implementation is already feature-complete
- **Update documentation** - Replace all `emergent-ctl` references with `emergent-cli ctl`

## Capabilities

### New Capabilities

None — this is a consolidation/cleanup change, not a feature addition.

### Modified Capabilities

- `cli-tool`: Remove bash duplicate script

## Impact

- **`deploy/minimal/emergent-ctl.sh`**: Deleted
- **`deploy/minimal/install-online.sh`**: Remove line 406 (bash script download)
- **`deploy/minimal/emergent-auth.sh`**: Update references to use `emergent-cli ctl restart` instead of `emergent-ctl restart`
- **`tools/emergent-cli/internal/cmd/ctl.go`**: No changes needed (already has all features)
- **Documentation**: Replace all `emergent-ctl` references with `emergent-cli ctl`

**Breaking changes**: **BREAKING** - Users must use `emergent-cli ctl <command>` instead of `emergent-ctl <command>`. Clean break, simpler architecture.
