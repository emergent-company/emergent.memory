## Why

Users who install a registry MCP server that needs a credential (for example GitHub's
`Authorization` header) are told only that a key is missing. The registry declares far
more guidance for each key — a description and, for some headers, a value template —
and every entry has a source repository that says where to create the credential. None
of that reached the configure UI: the configure banner named keys only, the value was a
free-form header the user had to prefix with `Bearer ` themselves, and an upstream 401
surfaced as a generic connection error.

## What Changes

- Install: the created server records registry provenance (registry name + repository
  URL).
- Configure form: the missing-key banner explains each key using the registry's
  description and links to the provider's credential docs (GitHub → token settings).
- Configure form: a required `Authorization` header renders a scheme picker (default
  `Bearer`, overridable) plus a token field; submit assembles `Bearer <token>`, and a
  blank token keeps the stored secret.
- Errors: an upstream HTTP 401/403 on connect/discover is reported as an actionable
  credential message rather than a generic failure.

## Capabilities

### Modified Capabilities

- `mcp-registry`: browse exposes each remote's declared credentials; install records
  provenance.
- `mcp-servers-ui`: banner descriptions + provider docs link; Authorization
  scheme+token input; actionable auth-rejection errors.

## Impact

- Server (`apps/server/domain/mcpregistry`): `RegistryRemoteDTO` carries headers/env;
  `MCPServer` gains `registry_name`, `registry_repository_url` (migration `00230`);
  provenance persisted on install; `outbound.go` classifies 401/403.
- Web UI (`apps/web-ui/gateway`): configure banner, KV row (auth variant),
  `net/url` docs link, registry listing layout (inline action column).
- Additive: existing manual servers have no provenance (no docs link) and no required
  config (no auth row).
