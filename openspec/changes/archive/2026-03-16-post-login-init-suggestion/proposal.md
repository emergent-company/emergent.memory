## Why

After `memory login` succeeds, the CLI prints a static message suggesting `memory status`. This is unhelpful — users in uninitialized folders need `memory init`, not `memory status`. Users in already-initialized folders would benefit from seeing their status immediately rather than running a separate command.

## What Changes

- Replace the static post-login hint (`Run 'memory status' ...`) with context-aware output
- When the current folder has no `.env.local` with `MEMORY_PROJECT_ID`: suggest `memory init` to set up a project
- When the current folder is already initialized: show inline authentication status plus the current project info (name and ID from `.env.local`)
- Add `godotenv` import to `auth.go` for reading `.env.local`

## Capabilities

### New Capabilities
- `post-login-context-hint`: Context-aware post-login output that checks folder initialization state and either suggests `memory init` or displays inline status with project info

### Modified Capabilities

## Impact

- `tools/cli/internal/cmd/auth.go` — `runLogin` function post-login output block (lines ~357–366)
- No API changes, no database changes, no breaking changes
- `godotenv` is already a dependency (used in `init_project.go`) — just needs importing in `auth.go`
