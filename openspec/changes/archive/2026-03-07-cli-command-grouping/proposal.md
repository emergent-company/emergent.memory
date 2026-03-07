## Why

The `memory` CLI exposes 25+ commands in a flat alphabetical list, making it hard for new users to understand what the tool does and for experienced users to find commands quickly. Commands for installing a self-hosted server appear alongside commands for querying knowledge bases — two completely different audiences and workflows.

## What Changes

- Add Cobra command groups to root: **Knowledge Base**, **Agents & AI**, **Account & Access**
- Move all server lifecycle commands (`install`, `upgrade`, `uninstall`, `ctl`, `doctor`) under a new `memory server` parent command
- Assign every top-level command to the appropriate group via `GroupID`
- Hide developer/diagnostic commands (`traces`, `db`) from default help output
- Move `projects` from Knowledge Base into Account & Access (projects are account-scoped resources)
- Move `mcp-guide` from auth into Agents & AI group
- Update root `Long` description to reflect the two usage modes

## Capabilities

### New Capabilities
- `cli-command-groups`: Cobra group definitions on root command and `GroupID` assignments across all command files
- `cli-server-subcommand`: New `memory server` parent command nesting `install`, `upgrade`, `uninstall`, `ctl`, `doctor`

### Modified Capabilities

## Impact

- `tools/cli/internal/cmd/root.go` — add `AddGroup` calls, update `Long` description
- `tools/cli/internal/cmd/auth.go` — add `GroupID: "account"` to login/logout/status/set-token; move mcp-guide to `GroupID: "ai"`
- `tools/cli/internal/cmd/projects.go` — add `GroupID: "account"`
- `tools/cli/internal/cmd/documents.go`, `graph.go`, `query.go`, `template_packs.go`, `blueprints.go`, `browse.go`, `embeddings.go` — add `GroupID: "knowledge"`
- `tools/cli/internal/cmd/agents.go`, `agent_definitions.go`, `adksessions.go`, `mcp_servers.go`, `provider.go`, `install_skills.go` — add `GroupID: "ai"`
- `tools/cli/internal/cmd/config.go`, `tokens.go` — add `GroupID: "account"`
- `tools/cli/internal/cmd/install.go`, `upgrade.go`, `uninstall.go`, `ctl.go`, `doctor.go` — remove `rootCmd.AddCommand`, re-register under `serverCmd`
- `tools/cli/internal/cmd/traces.go`, `db.go` — add `Hidden: true`
- New file: `tools/cli/internal/cmd/server.go` — defines `serverCmd` parent, wires 5 server subcommands
- No API changes, no breaking changes to existing command paths (only `install`/`upgrade`/`uninstall`/`ctl`/`doctor` paths change to `server *`)
