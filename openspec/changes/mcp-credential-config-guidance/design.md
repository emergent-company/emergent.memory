## Context

`mcp-required-config` gated registry installs on required credentials; a follow-up
carried each required key's registry description and header value template to the edit
form. This change closes the remaining "what do I actually provide?" gap across the
whole install → configure → recover loop.

## Goals

- Explain each missing key on the configure form and link to provider docs.
- Make the common `Authorization: Bearer <token>` case a scheme + token input.
- Attribute a 401/403 to the credential with an actionable message.

## Non-Goals

- OAuth (a separate change — `mcp-oauth-connect`).
- Backfilling provenance for already-installed servers.
- Changing the required/secret resolution rules or the remote-only install policy.

## Decisions

### Provenance is persisted, not re-looked-up

`kb.mcp_servers` gains nullable `registry_name` and `registry_repository_url`, written
by `InstallFromRegistry`. Manual creates leave them NULL and render no docs link, which
avoids a render-time registry lookup and keeps behavior stable if the catalog changes.

### Docs link is host-aware

`mcpCredentialDocs` maps a GitHub repository URL to the PAT settings page
(`https://github.com/settings/tokens`) and falls back to the repository for every other
host. This is a small curated map, deliberately not a general link resolver.

### Authorization is a scheme + token input

A required header named `Authorization` is special-cased on the form: a scheme picker
(`Bearer` default, derived from the registry value template when present, plus `Token`,
`Basic`, and raw) and a token field. Submit assembles `<scheme> <token>` (no double
prefix; raw passes through). The token field is only ever a placeholder — the stored
secret is write-only, so edits show a blank field whose blank re-submit keeps the
stored value.

### Auth rejection is typed at the transport

`authStatusTransport` wraps the outbound HTTP client and converts a 401/403 into
`ErrMCPAuthRejected{Status}`. The MCP client library otherwise masks the status as a
generic connection error. The message names the likely fix (`Bearer <token>`); the
gateway already renders backend error strings verbatim, so no new UI wiring is needed.

## Risks / Trade-offs

- `authStatusTransport` treats any 401/403 as terminal. For credential-bearing MCP
  requests that is correct; a server that legitimately 403s mid-session would surface
  the message, which is still the right user signal.
- The `.auth`/`.scheme` hidden fields must exist on every KV row (including JS-added
  rows) to keep the parallel form arrays index-aligned — covered by a mixed-row test.
