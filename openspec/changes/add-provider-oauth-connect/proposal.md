## Why

The first data-source iteration (`add-google-drive-ingestion`, merged #1625) hard-codes a single `google_drive` provider and asks the operator to paste an OAuth2 **refresh token** (§7.2 deferred the in-app authorize/callback). That is both poor UX and a dead end for the planned second provider (GitHub): there is no provider registry, no OAuth connect flow, and no place to hold an OAuth client. This change builds the **provider-generic auth + connect infrastructure** and moves Drive onto it, so a user just connects (no pasted secrets) and GitHub drops in as a second provider.

## What Changes

- **Provider registry**: replace the hard-coded `google_drive` switch with a registered provider interface (catalog metadata, config schema, credential schema, validation, connector factory, connection test, optional OAuth strategy). Adding a provider must not touch the CRUD/sync/HTTP core.
- **Generic OAuth connect**: `authorize` (state + PKCE S256) and `callback` (single-use state, code→token exchange) endpoints, provider-driven, storing tokens **encrypted, server-side** — no pasted refresh token.
- **OAuth client config**: per-provider client id/secret held at **org level** (encrypted, write-only), resolved with a deployment-env fallback (central app), so self-hosted deployments bring their own app.
- **Provider auth methods**: catalog advertises `oauth` vs `credentials` so the UI renders a Connect button or a credential form per provider.
- **Provider-generic token model**: credential blob is provider-defined JSON; the connector obtains a usable token at sync time (refresh-token exchange for Drive; long-lived/installation token for GitHub).
- **Web UI**: provider chooser, Connect-with-provider button + callback handling, org OAuth-client settings page, and a post-consent resource picker (Drive folder / GitHub repo).
- **Drive scope strategy**: support `drive.file` + Google Picker (verification-light, central app) and `drive.readonly` for internal/BYO apps — selectable per org client.
- **GitHub provider**: catalog entry + config/credential schema + connector contract defined; full GitHub connector implementation is a following phase.

## Capabilities

### New Capabilities

- `data-source-providers`: the server-side provider registry, generic OAuth connect flow, org-level OAuth client credentials, and provider-generic token/connector contract.
- `web-data-sources`: the provider-agnostic Data Sources UI — chooser, OAuth connect + callback, org OAuth-client settings, resource selection.

### Modified Capabilities

<!-- None that have a main spec. The predecessor change `add-google-drive-ingestion`
     is held and unarchived; this change supersedes its §7.2/§7.3 and is related to its
     `google-drive-source` capability, but does not MODIFY a main-spec capability. -->

## Impact

- `apps/server/domain/sources/**` — provider registry, OAuth service + routes, org-credential store, migration(s).
- `apps/server/internal/gdrive/**` — scope strategy; new `internal/github` connector package (following phase).
- `apps/server/domain/orgs/**` or a new provider-credentials domain — org-level encrypted OAuth client config.
- `apps/server/pkg/sdk/datasources/**` — provider `authMethod`, OAuth endpoints, org-credential endpoints.
- `apps/server/migrations/**` — new table for OAuth client config / oauth state.
- `apps/web-ui/gateway/**` (`datasources*.go`, `datasources.templ`) + `apps/web-ui/tests/e2e/**`.
- Config/env: deployment-default OAuth client id/secret per provider; redirect-URI base URL.
- No breaking change to ingested documents; Drive sources gain a token-based credential instead of a pasted one.
