## Why

The CLI has no `memory orgs` command despite the server exposing a full CRUD API (`/api/orgs`) and the SDK already supporting all four operations (`List`, `Get`, `Create`, `Delete`). Users who need to manage organizations — list them, create new ones, or delete old ones — must use the admin UI or raw API calls. Additionally, `memory init` does not check whether the user has an organization, so first-time users who lack one hit a confusing failure when trying to create a project.

## What Changes

- Add a new `memory orgs` command group with subcommands: `list`, `get`, `create`, `delete`
- Enhance `memory init` to detect when the user has no organization and offer to create one before project selection
- All org commands use account-level authentication (OAuth/account API key), same pattern as `memory projects`

## Capabilities

### New Capabilities
- `cli-orgs-crud`: CLI command group (`memory orgs`) with `list`, `get`, `create`, `delete` subcommands for organization management
- `init-org-check`: Enhancement to `memory init` that detects missing organizations and offers interactive creation before proceeding to project setup

### Modified Capabilities

## Impact

- **CLI code**: New file `tools/cli/internal/cmd/orgs.go`; modifications to `tools/cli/internal/cmd/init_project.go`
- **SDK**: No changes — `apps/server/pkg/sdk/orgs/client.go` already has all needed methods
- **Server**: No changes — `apps/server/domain/orgs/handler.go` already has all needed endpoints
- **Dependencies**: No new dependencies; uses existing `cobra`, `client`, `config` packages
