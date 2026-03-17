## Context

The Memory CLI (`tools/cli/`) uses Cobra for command structure and an SDK client (`apps/server/pkg/sdk/`) for API calls. The server already exposes a full CRUD API for organizations at `/api/orgs` (list, get, create, delete) and the SDK client (`c.SDK.Orgs`) already implements all four methods. The `memory projects create` command already calls `c.SDK.Orgs.List()` internally to resolve the org ID. However, there is no dedicated `memory orgs` command, and `memory init` does not check for org existence before entering the project flow.

The CLI follows a consistent pattern: each command group lives in a single file under `tools/cli/internal/cmd/`, self-registers via Go `init()` by calling `rootCmd.AddCommand()`, and uses either `getClient()` (project-scoped) or `getAccountClient()` (account-scoped) for authentication.

## Goals / Non-Goals

**Goals:**
- Add `memory orgs list|get|create|delete` commands following the same pattern as `projects.go`
- Enhance `memory init` to detect when user has zero orgs and offer to create one
- Support JSON output (`--json` / `--output json`) for all org subcommands
- Use account-level auth (OAuth / account API key) since orgs are not project-scoped

**Non-Goals:**
- Org member management (invite, remove, list members) — separate change
- Org settings or billing — not exposed in current API
- Renaming/updating organizations — no `PATCH` endpoint exists
- Non-interactive org creation via `memory init` flags — interactive-only for init

## Decisions

### 1. Single file for all org commands
All `memory orgs` subcommands will live in `tools/cli/internal/cmd/orgs.go`, matching the pattern of `projects.go`. This keeps the command group cohesive.

**Alternative**: Separate files per subcommand — rejected because no other command group does this and it adds unnecessary file proliferation.

### 2. Account-level authentication for all org commands
Org operations are not project-scoped, so all subcommands use `getAccountClient(cmd)` (OAuth or account API key), not project tokens.

**Alternative**: Allow project tokens with elevated scopes — rejected because the SDK's Orgs client is a non-context client (doesn't take orgID/projectID) and org operations are inherently account-level.

### 3. Init org check placement: before project selection
The org check in `memory init` will happen immediately after client creation, before the project picker. If no orgs exist, the user is prompted to create one. This ensures the subsequent `initSelectOrCreateProject` → `initCreateProject` → `resolveProviderOrgID` chain always has an org available.

**Alternative**: Check during project creation only — rejected because the failure happens deep in the call stack with a confusing error message.

### 4. `memory orgs list` as default (no subcommand)
Running `memory orgs` without a subcommand will show help (default Cobra behavior), consistent with `memory projects`.

### 5. Registration in "account" command group
The `orgs` command will use `GroupID: "account"` matching `projects`, since orgs are account-level resources.

## Risks / Trade-offs

- **[Risk] Org deletion is destructive** → The `delete` subcommand will require the org ID (not name) to reduce accidental deletion. A future improvement could add a confirmation prompt, but the server-side handler doesn't prevent deletion of orgs with active projects — this is an existing server limitation, not something the CLI should try to guard against.
- **[Risk] Some OAuth users see empty org list** → The existing fallback in `projects.go:548-551` (derive org from existing projects) will also be used in init. If truly no org and no projects exist, the user gets a clear prompt to create one.
