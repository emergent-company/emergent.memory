## Why

MCP OAuth connect (the archived `add-mcp-oauth-connect` change) stores exactly
one OAuth credential per remote MCP server, keyed by `server_id` — a
project-wide credential. Every tool call from every member of the project uses
that single account, and one member disconnecting clears the connection for the
whole project. Its own follow-ups (§9.1/#1783, §9.2/#1784) were deferred with
the note to design them together: per-user credentials change the store, the
proxy, and the connect/disconnect ownership model at once, and the CLI needs the
same connect/status/disconnect surface the web UI already has.

This change makes OAuth credentials **per-user with a shared fallback**: each
member who connects gets their own encrypted token set resolved at tool-call
time, while a pre-existing connected server (migrated as a `user_id IS NULL`
row) keeps working as the project-wide fallback. It also exposes
`authorize`/`status`/`disconnect` on the SDK and adds a
`memory agents mcp-servers oauth connect|status|disconnect` CLI command group.

## What Changes

- **Storage**: `kb.mcp_server_oauth` gains a nullable `user_id`; a row with
  `user_id IS NULL` is the shared/project-wide fallback, and there is at most one
  shared row and at most one row per `(server, user)`. Existing connected rows
  migrate in place as the shared row — no server is orphaned.
- **Resolution**: the proxy resolves the *calling* user's credential at tool-call
  time (threading the authenticated user through the request context), falling
  back to the shared row; device-targeted servers keep resolving the shared row
  (a device has no user).
- **Connect/disconnect ownership**: `authorize` binds the pending flow to the
  user and writes the resulting token set to that user's row; `disconnect`
  deletes only the caller's own row (a user can never delete another user's
  credential); `status` surfaces whose identity is connected (shared vs personal)
  without ever returning token material.
- **CLI parity**: `mcpregistry` SDK client gains `Authorize`/`Status`/`Disconnect`;
  `memory agents mcp-servers oauth connect|status|disconnect` wrap them, opening
  the browser for connect and polling `status` until the callback lands.
- **Security**: no endpoint ever returns a token; the store and proxy filter by
  user so one user cannot read or use another user's token; tokens are never
  logged.

## Capabilities

### New Capabilities

- `cli-mcp-oauth`: the CLI `mcp-servers oauth` command group (connect, status,
  disconnect) wrapping the server-side OAuth connect endpoints.

### Modified Capabilities

- `mcp-oauth`: per-user credentials with a shared fallback, per-caller
  resolution, and ownership-gated connect/disconnect/status.
- `mcp-registry`: the per-server authorization summary now reflects the calling
  user's connection state rather than a project-wide flag.

## Impact

- `apps/server/migrations/` — one migration (`00243`) adding `user_id` and the
  shared/per-user unique indexes.
- `apps/server/domain/mcpregistry` — entity, store, proxy (connection-pool keying
  and header resolution), service, handlers, DTOs, and tests.
- `apps/server/pkg/sdk/mcpregistry` — `Authorize`/`Status`/`Disconnect` methods
  and their types.
- `apps/cli/internal/cmd` — `mcp-servers oauth` command group + tests.
- No breaking change: a server with only the shared credential behaves exactly as
  today; new connects simply land in a per-user row instead of the shared row.
