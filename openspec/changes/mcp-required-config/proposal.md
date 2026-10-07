## Why

Registry-installed MCP servers can require credentials. The GitHub entry
(`io.github.github/github-mcp-server`, remote `https://api.githubcopilot.com/mcp/`)
declares an `Authorization` secret header. Memory currently installs it
`Enabled: true` with that header blank, silently fails discovery (401, logged only),
and never tells the user what to configure: `collectRegistryEnvVars` collects only
environment variables, never remote headers — so the post-install warning is empty.
Credential entry is possible on the edit form (secret key/value rows), but it is
undiscoverable and nothing prevents enabling a server that cannot work. There is also
no way to verify credentials before saving.

## What Changes

- Resolve required configuration from the **installed transport's** declaration
  (remote headers and env vars marked `isRequired` or `isSecret`).
- Persist it on the server (`required_config`) so the gate survives beyond install.
- Create registry installs **disabled** when required config is declared; return the
  keys so the UI can guide the user to configure them.
- Hard-gate enabling: reject enabling a server while a declared required key is blank
  (`400`, naming the keys).
- Surface "Needs configuration" on the list; highlight required rows on the edit form;
  redirect a needing-config install straight to that server's edit page.
- Add a save-less **Test connection** on the create and edit forms that probes the
  draft configuration (backfilling stored secrets in edit mode) without persisting.

## Capabilities

### Modified Capabilities

- `mcp-registry`: install resolves required config from the selected transport,
  persists it, creates the server disabled when unfilled, and returns the keys.
- `mcp-servers-ui`: needs-configuration badge + configure action; required-row
  affordance; enable is blocked until configured; save-less connection test.

## Impact

- Server (`apps/server`): migration `kb.mcp_servers.required_config`; `domain/mcpregistry`
  entity/service/handler/routes (+ unit tests).
- Web UI (`apps/web-ui/gateway`): `mcp_servers*.go/.templ`, `mcp_registry.go`,
  `backend.go`, `main.go` (+ unit tests), the memory client, and Playwright e2e.
- No change to the remote-only transport policy or the registry URL SSRF guard.
- Existing installed servers keep `required_config = []` and stay enabled; only new
  installs (and servers edited afterward) are gated.
