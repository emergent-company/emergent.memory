<!-- Baseline failures (pre-existing, not introduced by this change):
- None — CLI builds cleanly
-->

## 1. Add godotenv import to auth.go

- [x] 1.1 Add `"github.com/joho/godotenv"` to the import block in `tools/cli/internal/cmd/auth.go`

## 2. Replace post-login output in runLogin

- [x] 2.1 Replace the static `Run 'memory status'` hint (lines ~357-366) with folder initialization detection: read `.env.local` via `godotenv.Read(".env.local")` and check for `MEMORY_PROJECT_ID`
- [x] 2.2 Implement uninitialized branch: print `Run 'memory init' to set up a project in this folder.` when `MEMORY_PROJECT_ID` is absent or `.env.local` is missing
- [x] 2.3 Implement initialized branch: print inline authentication status block (Mode: OAuth, User email, Status: Authenticated) followed by current project line with name and ID from `.env.local`
- [x] 2.4 Handle edge case where `MEMORY_PROJECT_NAME` is missing — show only project ID in the project line

## 3. Verify

- [x] 3.1 Run `task build` to confirm the CLI compiles without errors
- [x] 3.2 Run `task test` on the cli package to ensure no regressions
