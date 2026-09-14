## Why

This project ships skills for multiple AI agents (opencode, generic `.agents` agents, etc.) but has no automated way for contributors or users to install those skills into the correct locations for their preferred tool. Today, skills must be discovered and copied manually, which is error-prone, tool-specific, and undocumented. A `runlog skills install` (or standalone `install-skills`) CLI command makes onboarding instant and self-documenting.

## What Changes

- New CLI command: `runlog skills install` (subcommand of the existing `runlog` binary in `cmd/runlog/`)
- Command discovers available skills from `.agents/skills/` (the canonical source) and `.opencode/skills/`
- Command discovers which AI agents are configured in the project (by detecting tool-specific config directories/files)
- Presents the user with a selection of target agents to install skills for
- Copies/symlinks skill files into the correct location for each selected agent tool
- Ships a reference map of known tools → skill install paths (matching the OpenSpec supported-tools table)
- Default install path: `.agents/skills/` (already the convention used by this project)
- Per-tool overrides documented and enforced (e.g., opencode → `.opencode/skills/`, claude → `.claude/skills/`)

## Capabilities

### New Capabilities

- `skills-install`: Interactive CLI command that detects configured agents, shows a picker, and installs runlog skills into the correct tool directories

### Modified Capabilities

- (none)

## Impact

- New code lives in `cmd/runlog/` (the existing CLI entry point for the `runlog` TUI binary)
- No new external dependencies — standard library only (consistent with project convention)
- Affects: `cmd/runlog/main.go` or a new `cmd/runlog/skills.go` subcommand file
- Read-only impact on `.agents/skills/` and `.opencode/skills/` source directories
- Writes into tool-specific directories when user confirms
