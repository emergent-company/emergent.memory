## Context

Registry install resolves the entry from the embedded catalog, picks a remote
transport (`streamable-http` > `sse`), and creates `kb.mcp_servers` enabled. The
transport declaration already carries the credential metadata
(`RegistryHeader{Name,IsRequired,IsSecret,Value}`, `RegistryEnvVar{...}`), but the
install path only inspects `EnvironmentVariables` and never headers.

## Goals / Non-Goals

- Goal: an installed server that cannot authenticate is visibly "needs configuration"
  and cannot be enabled until configured.
- Goal: verify credentials before saving.
- Non-Goal: changing the remote-only transport policy or the SSRF guard.
- Non-Goal: backfilling existing rows (they keep `required_config = []`).

## Frozen contract

### DB

`kb.mcp_servers.required_config jsonb NOT NULL DEFAULT '[]'`, shape:

```json
[{"name":"Authorization","kind":"header","isSecret":true,"isRequired":true}]
```

`kind` is `header` | `env`. `isRequired` is true when the registry declared
`isRequired` **or** `isSecret` (a secret with no value is unusable).

### Server DTOs

- `RequiredConfigKey{ Name string; Kind string; IsSecret bool; IsRequired bool }`.
- `MCPServerDTO` adds:
  - `requiredConfig []RequiredConfigKey` (`json:"requiredConfig"`)
  - `needsConfiguration bool` (`json:"needsConfiguration"`)
  - `missingConfigKeys []string` (`json:"missingConfigKeys"`) — names only.
- `InstallResultDTO` adds `requiredConfig []RequiredConfigKey`
  (`json:"requiredConfig"`); existing `requiredEnvVars` is retained for compatibility
  and still populated for env vars.

### Missing-key rule

A required key is missing when its value is blank AND it has no stored secret:

- `kind == "header"`: `Headers[name]` non-blank, or `name` present in `SecretHeaders`.
- `kind == "env"`: `Env[name]` non-blank, or `name` present in `SecretEnv`.

### Install

`selectTransport` returns the selected remote so `collectRegistryRequiredConfig`
can read that remote's `Headers` + `EnvironmentVariables`. When the list is non-empty,
`buildRemoteDTO` sets `Enabled = false` and install skips discovery; otherwise
`Enabled = true` and discovery runs as today.

### Enable gate

`Service.UpdateServer` returns `ErrRequiredConfigMissing{Keys []string}` (mapped to
`400`) when a request sets `Enabled = true` and `MissingRequiredConfigKeys(server)` is
non-empty. Creation is unaffected outside the registry path (manual creates carry an
empty required list).

### Test endpoint (save-less)

`POST /api/admin/mcp-servers/test`, body = `CreateMCPServerDTO` plus optional
`serverId`, response = the existing `MCPServerInspectDTO` envelope.

`Service.TestServerConfig(ctx, projectID, dto, serverID *string)`:

- Build a **transient** `MCPServer` (never persisted); validate transports as `CreateServer`.
- Draft secret rows arrive in plaintext: place them directly in `Env`/`Headers`.
  The proxy's `decryptEnv`/`decryptHeaders` are no-ops when `SecretEnv`/`SecretHeaders`
  are empty, so no encryption is needed and no secret is logged.
- Edit mode: for a required key left blank, backfill the stored value from `serverID`
  (`SecretEnv`/`SecretHeaders` copied verbatim into the transient server's secret maps).
- Reject `TargetDevice` (no outbound connection), matching `InspectServer`.
- Delegate to `ProxyManager.InspectServer` for the probe.

### Web UI

- Forms post to `/api/mcp-servers/test`; JS serializes the live form (including unsaved
  kv rows) and renders the result into the existing inspect area
  (`renderInspect`), never saving.
- Install redirects to `/settings/mcp-servers/{id}/edit?configure=1` when the result
  declares required config; otherwise the existing list redirect.

## Risks / Trade-offs

- Vendor under-declaration (secret flagged, or header omitted) means we cannot infer
  config need from the registry; the Test button is the fallback proof.
- The manual-create path allows arbitrary URLs to admins today; the test endpoint
  matches that trust model and does not add the registry-only SSRF guard.
- Draft test values traverse the browser → gateway → memory in cleartext (same as
  save); they are never logged and never persisted.
