## Why

When working inside a project folder (where `.env.local` exports `EMERGENT_PROJECT_TOKEN`), users have no passive indication of which project is active before a command executes. This creates ambiguity — especially when switching between multiple project directories — and can lead to accidental mutations against the wrong project. A brief, consistent header on every CLI command output that identifies the active project removes this guesswork.

## What Changes

- Every CLI command that loads a project context (via `EMERGENT_PROJECT_TOKEN` / config / flag) will print a one-line project indicator to `stderr` before its output.
- The indicator shows the project name (when available) and/or ID, along with the source (env file, env var, flag, config).
- The display is skipped when: no project token is set, `--quiet` / `-q` flag is passed, or stdout is not a TTY (i.e., piped output).
- The existing `resolveProjectContext()` stderr banner in `helpers.go` is refactored/extended to be the single place this indicator is emitted, triggered via a `PersistentPreRunE` on the root command.

## Capabilities

### New Capabilities

- `cli-project-context-indicator`: A passive, per-command project context header printed to stderr whenever a project token is active, giving users continuous visibility into which project they are targeting.

### Modified Capabilities

- `cli-root-command`: The root Cobra command gains a `PersistentPreRunE` hook that resolves the project context (name + source) and prints the indicator, replacing the ad-hoc stderr prints currently scattered across individual commands.

## Impact

- `tools/emergent-cli/internal/cmd/root.go` — add `PersistentPreRunE`
- `tools/emergent-cli/internal/cmd/helpers.go` — extend/refactor `resolveProjectContext()` to return structured context info used by the hook
- `tools/emergent-cli/internal/config/config.go` — no schema changes; read-only usage of existing `ProjectToken`, `ProjectID`, `ProjectName` fields
- No API changes, no database changes, no breaking changes
- Quiet mode (`--quiet` flag or non-TTY) suppresses the indicator — no impact on scripted/piped usage
