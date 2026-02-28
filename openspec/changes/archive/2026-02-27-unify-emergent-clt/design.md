## Context

The Emergent standalone installation uses Docker Compose to run the server locally. Currently, there are **two separate tools** providing identical service control functionality:

1. **`emergent-ctl.sh`** - A 203-line bash script at `deploy/minimal/emergent-ctl.sh` that wraps `docker compose` commands
2. **`emergent-cli ctl`** - A Go subcommand in `tools/emergent-cli/internal/cmd/ctl.go` (299 lines) that wraps the same `docker compose` commands

Both provide: `start`, `stop`, `restart`, `status`, `logs`, `health`, `shell`. The installer downloads the bash script to `~/.emergent/bin/emergent-ctl`. The bash script even delegates to the Go CLI for some commands (line 111: `docker exec emergent-server emergent`).

This duplication arose organically — the bash script was created first for the minimal deployment, then the `ctl` subcommand was added to `emergent-cli` without removing the original script.

## Goals / Non-Goals

**Goals:**

- Remove the duplicate bash script (`emergent-ctl.sh`)
- Use only `emergent-cli ctl` for all service control operations
- Update installer and all references to use `emergent-cli ctl`
- Maintain all existing functionality (no feature changes)

**Non-Goals:**

- No changes to `emergent-cli ctl` implementation (it already works)
- No backwards compatibility via symlinks or aliases
- No migration script for existing users (breaking change is acceptable for consolidation)

## Decisions

### Decision 1: Delete bash script, keep only Go implementation

**Rationale**: The Go implementation in `emergent-cli ctl` is already feature-complete and cross-platform. It provides better error handling, structured output, and integrates with the rest of the CLI tooling. The bash script was a temporary bridge that's no longer needed.

**Alternatives considered**:

- Keep bash script and remove Go: Would require maintaining bash code and lose integration with CLI auth/config system
- Symlink `emergent-ctl` → `emergent-cli`: Adds complexity; cleaner to have one canonical command

**Decision**: Delete `deploy/minimal/emergent-ctl.sh` entirely.

### Decision 2: Breaking change without migration path

**Rationale**: This is a pre-1.0 product with limited production deployments. The change is simple (add `cli` before `ctl`), and the blast radius is small. Adding symlinks or migration tooling adds complexity without sufficient benefit.

**Alternatives considered**:

- Symlink for backwards compatibility: Keeps the confusion alive, doesn't force users to learn the correct command
- Keep both: Perpetuates the duplication problem

**Decision**: Clean break — users must use `emergent-cli ctl <command>` going forward.

### Decision 3: Update installer to not download bash script

**Rationale**: `deploy/minimal/install-online.sh` line 406 downloads `emergent-ctl.sh` from GitHub releases. This line should be removed. The installer already installs `emergent-cli`, so no additional setup is needed.

**Implementation**: Remove `emergent-ctl.sh` from the `BIN_FILES` array in the installer.

## Risks / Trade-offs

**Risk**: Existing users with scripts calling `emergent-ctl` will break.
→ **Mitigation**: Document in release notes with clear migration guide. Pre-1.0 software, breaking changes expected.

**Risk**: `emergent-auth.sh` script references `emergent-ctl restart` (lines 235-236).
→ **Mitigation**: Update these references to `emergent-cli ctl restart` in the same commit.

**Trade-off**: Users must type `emergent-cli ctl` (17 chars) instead of `emergent-ctl` (12 chars).
→ **Accepted**: Shell aliases (`alias ectl='emergent-cli ctl'`) are user preference, not system responsibility. Clarity over brevity.

**Risk**: Documentation may reference `emergent-ctl` in multiple places.
→ **Mitigation**: Search all markdown files for `emergent-ctl` and replace with `emergent-cli ctl`. Include in PR checklist.

## Migration Plan

1. **Delete files**:

   - Remove `deploy/minimal/emergent-ctl.sh`

2. **Update installer**:

   - Edit `deploy/minimal/install-online.sh` line 406: remove `emergent-ctl.sh` from `BIN_FILES` array

3. **Update references**:

   - Edit `deploy/minimal/emergent-auth.sh`: replace `emergent-ctl restart` with `emergent-cli ctl restart` (lines 49, 235-236)
   - Search and replace `emergent-ctl` with `emergent-cli ctl` in all documentation

4. **Release notes**:

   - Document breaking change with before/after examples
   - Highlight that `emergent-cli ctl` provides identical functionality

5. **Rollback**: If needed, revert commit and redeploy previous installer. No data migration required.

## Open Questions

None — this is a straightforward consolidation with no technical ambiguity.
