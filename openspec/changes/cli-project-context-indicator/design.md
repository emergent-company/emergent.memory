## Context

The CLI has no persistent "which project am I in?" signal for the user. When a project token is active — either via `EMERGENT_PROJECT` (alias) or `EMERGENT_PROJECT_TOKEN` — every command silently uses that scope. Users working across multiple project directories have no visual confirmation they are targeting the right project until they see command results.

`initConfig()` in `root.go` already loads `.env.local` / `.env` before any command runs, so the project token is available process-wide by the time any `PreRun` hook fires. `LoadWithEnv` in `config.go` already resolves `EMERGENT_PROJECT` → `cfg.ProjectToken`. `cfg.ProjectName` is also populated from `EMERGENT_PROJECT_NAME` when present.

Current state: no `PersistentPreRunE` exists on the root command. Each command calls `getClient()` / `resolveProjectContext()` independently. The only existing stderr banner is inside `resolveProjectContext()` in `helpers.go`, which only fires for commands that explicitly call it (mostly document/object commands), not for all commands.

## Goals / Non-Goals

**Goals:**
- Print one line to `stderr` before every command's output when a project token is active
- Show project name when available, fall back to the token value (masked) or project ID
- Show the source (`EMERGENT_PROJECT`, `EMERGENT_PROJECT_TOKEN`, `--project-token` flag, config file)
- Suppress output when stdout is not a TTY (piped/scripted usage)
- No impact on stdout — indicator always goes to stderr

**Non-Goals:**
- Changing how the project token is resolved or authenticated (separate bug)
- Filtering or restricting which commands are available based on project scope (separate work)
- Showing the indicator for commands that run before config is loaded (e.g., `emergent version`, `emergent help`)

## Decisions

### 1. `PersistentPreRunE` on the root command

Add a `PersistentPreRunE` to `rootCmd` in `root.go`. This runs before every subcommand and has access to the fully-initialized viper/config state (since `cobra.OnInitialize` fires before `PreRun`).

Alternative considered: printing in `initConfig()` directly. Rejected — `initConfig` is a plain function with no access to the cobra command tree, making it impossible to skip for `version`/`help`/`completion` commands.

Alternative considered: adding to each command individually. Rejected — this is exactly the scattered pattern we want to eliminate.

**Skipped for:** `version`, `completion`, and any command where `cfg.ProjectToken == ""`. Detected via `cmd.CommandPath()` or by simply checking the token after config load.

### 2. Source detection

The source string is determined by which env var / flag was the origin:

| Condition | Source label |
|---|---|
| `--project-token` flag changed | `--project-token flag` |
| `EMERGENT_PROJECT_TOKEN` in env | `EMERGENT_PROJECT_TOKEN` |
| `EMERGENT_PROJECT` in env | `EMERGENT_PROJECT` |
| config file | `config file` |

Detection order: check `cmd.Flags().Changed("project-token")` first, then `os.Getenv("EMERGENT_PROJECT_TOKEN")` (set directly, not promoted from alias), then `os.Getenv("EMERGENT_PROJECT")`, then fall back to "config file".

### 3. Display format

```
Project: emergent.memory-dev  [EMERGENT_PROJECT]
```

Single line, stderr, using the project name when available (`cfg.ProjectName`), otherwise the resolved token source string. Kept deliberately minimal — not a full status panel, just a contextual breadcrumb.

Color: dim/muted using the existing `lipgloss` / color utilities already in the `ui` package, respecting `--no-color` / `NO_COLOR`.

### 4. TTY guard

Use `isatty` (already available transitively in the module) or `os.Stderr.Stat()` to check if stderr is a terminal. If not a TTY, skip the indicator entirely — keeps piped and CI usage clean.

## Risks / Trade-offs

- **Cobra `PersistentPreRunE` chaining**: If any subcommand defines its own `PersistentPreRunE`, cobra does not automatically chain them — it runs only the deepest one. Mitigation: audit existing subcommands for `PersistentPreRunE` usage (none found currently); document this constraint.
- **`version` / `help` noise**: These commands have no project context and should not show the indicator. Mitigation: check `cfg.ProjectToken == ""` — these commands never set a token so the guard fires naturally without special-casing.
- **Performance**: `LoadWithEnv` is called once in `PersistentPreRunE` and again inside `getClient()` for most commands. Minor duplication. Acceptable for now; a shared config singleton could be introduced later.

## Migration Plan

1. Add `PersistentPreRunE` to `rootCmd` — pure addition, no existing behaviour removed
2. Remove the redundant stderr banner from `resolveProjectContext()` in `helpers.go` to avoid double-printing
3. Build and install locally to verify
4. No rollback needed — if suppressed via `--no-color` or non-TTY, output is identical to current behaviour

## Open Questions

- Should the indicator also show the project ID alongside the name? (Could be toggled with `--debug`)
- Should `emergent status` suppress the indicator since it already prints full project context? Likely yes — check `cmd.Use` or `cmd.CommandPath()`.
