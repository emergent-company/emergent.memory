## 1. Database Migration

- [x] 1.1 Create a new database migration file to drop `langfuse_observation_id` and `langfuse_trace_id` from the `monitoring` table.
- [x] 1.2 Apply the migration to the local database.

## 2. Go Backend Cleanup

- [x] 2.1 Remove all Go files and directories related to the Langfuse integration from `apps/server-go`.
- [x] 2.2 Remove any Langfuse-related mock data or test configurations from the Go tests.
- [x] 2.3 Ensure the Go application builds and all tests pass after the removal.

## 3. Configuration and Script Cleanup

- [x] 3.1 Remove all `LANGFUSE_` and `SIGNOZ_` environment variables from `.env` and `.env.example`.
- [x] 3.2 Delete the `scripts/mcp-langfuse-wrapper.sh` script.
- [x] 3.3 Remove any Signoz-related configurations from `opencode.jsonc` and `.vscode/mcp.json`.

## 4. Documentation Cleanup

- [x] 4.1 Remove all documentation files related to Langfuse and Signoz from the `docs/` directory.
- [x] 4.2 Update `README.md`, `GEMINI.md`, `SETUP.md` and any other relevant documentation to remove references to Langfuse and Signoz.
