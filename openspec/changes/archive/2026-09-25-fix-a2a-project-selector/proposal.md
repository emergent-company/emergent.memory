# Send the A2A project selector and map a missing project to 400

## Why

Two related defects made the authenticated A2A surface unusable and misdiagnosed (issues #761, #762):

- The A2A SDK client (`apps/server/pkg/sdk/a2a/client.go`) sent no project selector, so `GET /extendedAgentCard` failed and `memory a2a discover` — the documented way to obtain the `--agent <skill-id>` that `memory acp` requires — never worked. The route comment claimed project addressing was "credential-scoped (token-bound), never path-scoped", which contradicted the endpoint's actual behaviour.
- `a2aErrorFromStatus` collapsed every non-401/403 status into `INVALID_AGENT_RESPONSE` (whose `HTTPStatus()` is 500), so a missing project selector looked like a server fault and sent the investigation of #761 in the wrong direction.

## What Changes

- The A2A client gains a `projectID` field with `NewClientWithProject` / `WithProject`, and sets `X-Project-ID` on every request — `doJSON` and the SSE `StreamMessage` path.
- The CLI threads the selected project into the A2A client. The global `--project` flag overrides the configured project, matching every other CLI command.
- `acpProjectID` — shared by the whole authenticated A2A surface — returns an explicit 400 `INVALID_ARGUMENT` / reason `PROJECT_REQUIRED` naming `X-Project-ID` when no project selector is present, and a project-scoped `emt_*` token binds the request to its own project (the header cannot redirect it to another project).
- `a2aErrorFromStatus` maps 400 → `INVALID_ARGUMENT`, 404 → `NOT_FOUND`, 405 → `METHOD_NOT_ALLOWED`, keeps 401/403 as before, and reserves `INVALID_AGENT_RESPONSE` for 5xx only.
- Streaming A2A routes (`/message:stream`, `/tasks/{id}:subscribe`) convert handler-returned A2A errors into the A2A envelope instead of leaking to the generic 500 error handler.
- The `a2a_routes.go` route comment is corrected to describe header/token selector addressing.

## Capabilities

### Modified Capabilities

- `a2a-discovery`: authenticated A2A endpoints require a project selector; the extended card advertises the resolved project's external agents.
- `a2a-conformance`: A2A error reasons distinguish client errors from genuine server faults.

## Impact

- `apps/server/pkg/sdk/a2a/client.go` + `client_test.go`
- `apps/cli/internal/cmd/a2a.go` + `a2a_test.go`
- `apps/server/domain/agents/a2a_discovery.go`, `a2a_errors.go`, `a2a_routes.go`, `a2a_discovery_test.go`, `a2a_routes_test.go`

The unauthenticated `GET /.well-known/agent-card.json` path is unchanged; a client with no configured project still sends no header.
