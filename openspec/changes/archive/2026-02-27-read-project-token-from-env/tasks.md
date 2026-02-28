## 1. Setup

- [x] 1.1 Add `github.com/joho/godotenv` to Go dependencies

## 2. CLI Implementation

- [x] 2.1 Implement environment variable loading in `tools/emergent-cli/internal/cmd/root.go`
- [x] 2.2 Add logic to check for `EMERGENT_PROJECT_ID` and `EMERGENT_PROJECT_TOKEN` in loaded environment and make them globally available
- [x] 2.3 Implement precedence logic: Flag > Environment Variable
- [x] 2.4 Add informative output when context is loaded from `.env` or `.env.local` (e.g., printing `EMERGENT_PROJECT_NAME`)
- [x] 2.5 Create a new `projects` command group (if it doesn't exist) and add a `set` subcommand
- [x] 2.6 Implement API call in `set` command to fetch projects and interactive prompt for selection (when no args provided)
- [x] 2.7 Implement logic to parse project name/ID from args and resolve it via API
- [x] 2.8 Implement token retrieval/generation via API for the selected project
- [x] 2.9 Implement writing/updating `EMERGENT_PROJECT_TOKEN`, `EMERGENT_PROJECT_ID`, and `EMERGENT_PROJECT_NAME` in the `.env.local` file
- [x] 2.10 Update `tools/emergent-cli/internal/cmd/traces.go` to optionally use `resolveProjectID` and append `.project.id` to TraceQL queries

## 3. Verification

- [x] 3.1 Create a temporary `.env` file with `EMERGENT_PROJECT_ID` and verify CLI uses it for project-scoped commands
- [x] 3.2 Create a temporary `.env.local` file and verify it overrides `.env`
- [x] 3.3 Verify command-line flag `--project-id` overrides both
- [x] 3.4 Verify CLI works without project context files when flag is provided
- [x] 3.5 Verify `emergent projects set` lists projects interactively and saves token/ID/name to `.env.local`
- [x] 3.6 Verify `emergent projects set <id>` skips interactive prompt and saves token/ID/name to `.env.local`
- [x] 3.7 Verify `emergent traces list` filters by project if `EMERGENT_PROJECT_ID` is set locally
