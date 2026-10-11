## Context

`domain/sources` today is a single-provider implementation: the provider catalog, config schema, validation, credential extraction, connection test, and the sync worker's `DriveClient`/`DriveClientFactory` (`worker.go:24-32`) are all hard-coded to `google_drive`. Credentials are an OAuth2 `{client_id, client_secret, refresh_token}` struct encrypted with `pkg/crypto` and stored on the source row (`kb.sources.encrypted_credentials`). The predecessor change deferred the in-app authorize/callback (§7.2) and additional connectors (§7.3). The next source is GitHub. This change is the provider-generic infrastructure both need.

## Goals / Non-Goals

**Goals**
- One registry/interface that any provider implements; adding GitHub touches only a new package + registration.
- Users connect via OAuth (no pasted secrets); tokens are obtained and refreshed by the server.
- OAuth clients are configurable per org (BYO) with a deployment/env default (central app).
- Auth method is advertised per provider so the UI is provider-agnostic.
- Drive remains the first provider and is migrated onto the new flow.

**Non-Goals**
- Shipping the full GitHub connector in this change (catalog/schema/contract yes; enumeration/sync implementation is the following phase).
- Migrating existing pasted-refresh-token Drive sources automatically beyond a documented path (a source can be re-connected).
- Building a general OAuth broker for arbitrary third parties.

## Decisions

### D1 — Provider registry interface, not a switch
Introduce a `Provider` interface (server-side), one implementation per `providerType`, registered in an `fx` group. It exposes:
- `Catalog() Provider` — type, name, description, sourceType, icon, availability.
- `ConfigSchema() ProviderSchema`, `CredentialSchema() []Field` (write-only fields).
- `ValidateConfig(config) error`, `ExtractCredentials(config) (map, error)`.
- `AuthMethods() []AuthMethod` — `oauth` and/or `credentials`.
- `OAuth() (OAuthStrategy, bool)` — authorize-URL builder + code exchange + scopes + token extraction.
- `NewConnector(ctx, creds, config) (Connector, error)` where `Connector` is the generalized `DriveClient` (`Enumerate` + `Fetch`), and `TestConnection(ctx)`.

Core (`service.go`, `worker.go`, `routes.go`) resolves a provider by type and never branches on provider name. **Alternative rejected:** keep the switch and add GitHub — O(providers) edits in core, every new provider risks the sync path.

### D2 — Generic OAuth connect (authorize + callback)
Two endpoints, provider-parameterised:
- `GET /api/data-source-integrations/providers/:type/oauth/authorize` → builds the provider authorize URL with `state` (opaque, random) and PKCE `code_challenge` (S256), returns `{authorizeUrl}` (or 302).
- `GET /api/data-source-integrations/oauth/:type/callback?code&state` → verifies `state` (single-use, TTL-bound, bound to project + provider + initiating user + intended config), exchanges `code` (with `client_secret` + PKCE `code_verifier`) for tokens, encrypts and stores them on the source, then redirects to the UI.

`state` + pending PKCE/config are persisted (short TTL, e.g. 10 min) server-side — a `kb.oauth_pending_states` table (or cache + DB fallback). **Why:** OAuth callbacks are cross-site; the pending record is the CSRF and flow-continuity anchor. **Alternative rejected:** stateless signed state — loses the PKCE verifier and the intended-config binding, and can't be single-use.

### D3 — OAuth client config: org-level BYO with deployment default
`client_id`/`client_secret` move out of the per-source payload into an **org-scoped, per-provider** encrypted config (`kb.oauth_client_configs`), resolved as: org config → deployment env (`MEMORY_<PROVIDER>_OAUTH_CLIENT_ID/SECRET`) → error. Redirect URI is derived from a configured public base URL and must be registered on the app. **Why:** a pasted secret per source is unmanageable; BYO avoids Google verification for self-hosted orgs; a central app (env) keeps the hosted SaaS zero-setup.

### D4 — Provider-generic token model
The stored credential blob is provider-defined JSON. The connector is responsible for obtaining a usable access token at sync/test time:
- Google: `{refresh_token, client_id, client_secret?}` → refresh-token exchange (client secret may come from D3).
- GitHub: OAuth App long-lived token, or GitHub App `{installation_id, ...}` → minted installation token (1h).

So the core stores/encrypts an opaque blob; only the provider interprets it. **Why:** Google refresh tokens and GitHub tokens/installations have different lifetimes and renewal; a lowest-common-denominator schema would leak provider specifics into core.

### D5 — Auth method advertised per provider
`Provider.authMethod` ∈ `oauth | credentials | both` drives the UI: `oauth` → "Connect with X"; `credentials` → the existing credential form (kept for providers with no OAuth, or BYO-without-connect). **Why:** keeps the connect page provider-agnostic.

### D6 — Drive scope strategy (Picker vs readonly)
Support two scope strategies chosen by the org client config:
- `drive.file` + **Google Picker** — app sees only files the user picks; **no restricted-scope verification**, best for a central/published app. Requires a Picker developer key + client-side Picker and a post-consent folder selection.
- `drive.readonly` — simplest server-side enumeration; viable for **internal/BYO org apps** (Workspace-internal apps avoid Google review), restricted-verification-hostile for a central app.

**Why:** the previous code hard-codes `drive.readonly` (`client.go:36`), which is a restricted scope — the exact thing that makes a shared app's Google review unpredictable. Making the strategy configurable lets each deployment pick the trade-off.

### D7 — Post-consent resource selection
After the callback grants a token, the UI selects the resource the source will sync: a Drive **folder** (Picker for `drive.file`, or a server-listed folder tree for `drive.readonly`) or a GitHub **repo** (+ path glob, ref). Selection is provider-driven (`ListResources`), and the chosen id is stored in the source `config`. **Why:** the token is not enough to define what to sync; the resource is user-chosen after consent.

### D8 — GitHub auth model
Support **GitHub OAuth App** first (authorize `github.com/login/oauth/authorize`, scopes `repo`/`public_repo` + `read:org`, PKCE), returning a long-lived token (no refresh). The provider interface also models GitHub **App** installation credentials for a later phase. Sync target: a repo's markdown/docs (+ optionally issues). **Why:** OAuth App is the least-friction BYO path and shares the generic Google flow; GitHub App is stricter but more setup.

### D9 — UI provider-agnostic
`/data-sources/new` lists providers; the connect page renders per provider (OAuth button or credential form) from the catalog + schemas; org admins get an OAuth-client settings page; the callback returns to the detail page with a flash. Adding GitHub = a catalog entry, no new page.

## Risks / Trade-offs

- **State/PKCE storage**: extra table + TTL sweeper. *Mitigation*: short TTL + lazy expiry; a failed/already-consumed state never stores a token.
- **Redirect-URI misconfig** (the #1 OAuth support cost): surfaced as an actionable error; documented per deployment; the authorize builder validates the base URL is set.
- **`drive.file` requires the resource to be picked and then re-picked if moved** — acceptable; `drive.readonly` remains for internal apps.
- **Center-app verification**: not solved by us; BYO keeps it off the critical path; `drive.file`+Picker is the central-app-friendly option.
- **Migration of existing pasted-token sources**: they keep working (same encrypted blob shape); the UI offers "reconnect". No forced migration.

## Migration Plan

1. Add the provider registry + register `google_drive` behind it (no behavior change).
2. Add D3 tables (oauth client configs, pending states) via Goose migration.
3. Add OAuth endpoints + org-credential endpoints; SDK + gateway methods.
4. Add the UI (chooser, Connect, settings, resource selection); ship Drive OAuth end-to-end; keep the pasted-credential form as the `credentials` fallback.
5. Register a `github` provider catalog entry + schemas (connector follow-up).
6. Rollback: the OAuth endpoints and UI are additive; reverting the PR returns to the pasted-credential flow.

## Open Questions

- Default scope strategy for the hosted SaaS: `drive.file`+Picker (privacy + review) vs `drive.readonly` (simplicity)? Recommend `drive.file`+Picker.
- GitHub sync scope (docs only vs docs+issues) — defer to the GitHub-connector phase.
- One OAuth client config per provider per org, or per project? Recommend per **org** (matches "configure the app in their organization").
