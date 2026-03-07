## Context

The `memory` CLI (`tools/cli/`) is built with Cobra v1.10.2 + Viper. All ~25 top-level commands self-register via their own `init()` functions calling `rootCmd.AddCommand(...)` in `tools/cli/internal/cmd/`. There is no grouping — commands appear in a flat alphabetical list.

Cobra v1.6+ supports native command groups via `rootCmd.AddGroup()` and `cmd.GroupID`. Groups are purely a display concern: they affect only `--help` output and do not change command routing or invocation paths.

Server lifecycle commands (`install`, `upgrade`, `uninstall`, `ctl`, `doctor`) currently live at the top level alongside API-usage commands, creating noise for the majority of users who connect to a hosted instance.

## Goals / Non-Goals

**Goals:**
- Group all top-level commands into labeled sections in `--help` output
- Move server lifecycle commands under `memory server` to reduce top-level noise
- Hide developer/diagnostic commands (`traces`, `db`) from default help
- Keep all changes purely cosmetic/structural — no behavior changes to existing commands

**Non-Goals:**
- Changing any command's flags, arguments, or runtime behavior
- Adding or removing functionality from any existing command
- Changing shell completion behavior
- Changing config file format or env var handling

## Decisions

### 1. Cobra native groups over custom help template

Cobra v1.10.2 has `AddGroup()` / `GroupID` built in. The alternative is overriding `rootCmd.SetHelpTemplate()` with a custom template. Native groups are preferred: they integrate with tab-completion, `--help` flag on subcommands, and don't require maintaining a fragile string template.

### 2. `memory server` as a real parent command, not an alias group

Server commands are moved under a genuine `memory server` parent (new `server.go` file) rather than just visually grouped. This means:
- `memory server install` replaces `memory install`
- `memory server ctl start` replaces `memory ctl start`
- etc.

Alternative considered: keep server commands at root but assign them to a "Server" group. Rejected because the user explicitly wants server commands de-emphasized for non-server-operators, and a real subcommand namespace achieves that more clearly.

### 3. `projects` in Account & Access group

Projects are account-scoped resources (belong to an org, gated by auth) rather than knowledge content. Placing them in Account & Access alongside `tokens`, `config`, `login` reflects the mental model of "things you set up before doing knowledge work."

### 4. `traces` and `db` hidden, not removed

`Hidden: true` on the Cobra command struct removes them from `--help` output but keeps them fully functional. This preserves developer workflows without cluttering the help for end users.

### 5. `mcp-guide` moves to Agents & AI

`mcp-guide` is a setup guide for AI agent integrations — not an auth/account concern. It was in `auth.go` for historical reasons (token-related setup). Moving its `GroupID` to `"ai"` corrects the mental model without moving the code.

## Risks / Trade-offs

- **Breaking change for server operators** — `memory install`, `memory ctl`, `memory doctor`, etc. all change paths to `memory server install`, `memory server ctl`, `memory server doctor`. Any scripts or docs referencing the old paths break. Mitigation: document in release notes; old paths are not preserved.
- **Shell completion** — users with existing completions cached may get stale results until they re-run `memory completion`. Mitigation: note in release.
- **`server.go` file is a thin dispatcher** — the 5 server subcommands are re-registered under `serverCmd` rather than `rootCmd`. The command var names and logic in their respective files stay unchanged; only the `AddCommand` target changes.

## Migration Plan

1. Create `server.go` with `serverCmd` parent
2. In each of `install.go`, `upgrade.go`, `uninstall.go`, `ctl.go`, `doctor.go`: change `rootCmd.AddCommand(...)` → `serverCmd.AddCommand(...)` (called from `server.go`'s `init()`)
3. Add `AddGroup` calls to `root.go`
4. Add `GroupID` to each command's struct definition
5. Set `Hidden: true` on `tracesCmd` and `dbCmd`
6. Update root `Long` description
7. Build and verify `memory --help` output matches expected layout

Rollback: revert the file changes — no DB migrations, no API changes, no external dependencies touched.
