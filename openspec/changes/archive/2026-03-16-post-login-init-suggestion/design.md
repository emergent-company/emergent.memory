## Context

After `memory login` completes successfully, the CLI currently prints a static hint: `Run 'memory status' to see your account and available projects.` This message is shown regardless of folder state. The actual next step depends on whether the current directory already has a Memory project initialized (`.env.local` containing `MEMORY_PROJECT_ID`).

The `runLogin` function in `tools/cli/internal/cmd/auth.go` handles the full login flow. Post-login output is at lines ~357–366. The `godotenv` package is already a Go module dependency (used in `init_project.go` in the same package).

## Goals / Non-Goals

**Goals:**
- After login, detect whether the current folder has an initialized Memory project
- If not initialized: suggest `memory init` instead of `memory status`
- If initialized: display inline authentication status and current project info (name + ID from `.env.local`)

**Non-Goals:**
- Automatically running `memory init` after login
- Fetching project info from the server (use local `.env.local` values only — avoids extra round-trip with the OAuth token)
- Changing the `memory status` command itself
- Handling the API-key login path (only OAuth login via `runLogin` is affected)

## Decisions

### Decision 1: Read `.env.local` directly with `godotenv` instead of calling `runStatus`

**Choice:** Read `.env.local` using `godotenv.Read()` and branch on `MEMORY_PROJECT_ID` presence, then print status + project info inline.

**Alternative considered:** Call `runStatus(cmd, args)` directly for the initialized case. Rejected because `runStatus` in OAuth mode only shows auth info (Mode/User/Issuer/Status) without project info, and re-loading config + credentials from disk that were just written is wasteful. Printing inline lets us combine auth info with project context from `.env.local` in a single coherent block.

### Decision 2: Use `.env.local` values for project info instead of server fetch

**Choice:** Read `MEMORY_PROJECT_NAME` and `MEMORY_PROJECT_ID` from `.env.local` for the project info line.

**Alternative considered:** Use the access token to call `/api/projects` and show full project details. Rejected because the OAuth access token isn't formatted for `setAuthHeader` (it uses Bearer, not X-API-Key), and the local `.env.local` already has the name and ID — a server call adds latency for no extra value in this context.

### Decision 3: Graceful fallback when `.env.local` is unreadable

**Choice:** If `godotenv.Read(".env.local")` returns an error or the key is missing, treat the folder as uninitialized and suggest `memory init`. No error is surfaced to the user.

**Rationale:** `.env.local` not existing is the common "not initialized" case. There's no need to distinguish between "file missing" and "file exists but no MEMORY_PROJECT_ID".

## Risks / Trade-offs

- **[Stale `.env.local`]** → The project name/ID in `.env.local` could be stale if the server project was deleted. Mitigation: this is informational only; the user will discover stale state when they run any project-scoped command. Same behavior as current `memory init` re-run detection.
- **[CWD mismatch]** → User may login from a directory that isn't their project root. Mitigation: suggesting `memory init` in a non-project folder is the correct behavior — it guides the user to set up. No harm done.
