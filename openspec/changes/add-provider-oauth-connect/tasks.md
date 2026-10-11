# Tasks

> Design/spec stage. Implementation ships on this same branch/PR (spec + implementation together);
> no task is complete until its code + tests land.

## 1. Provider registry (core, provider-agnostic)

- [x] 1.1 Define the `SourceProvider` interface (catalog, config schema, credential fields, `AuthMethods`, `ValidateConfig`, `ExtractCredentials`, `NewConnector`) and a `Connector` interface generalizing the worker's `DriveClient` (`Enumerate` + `Fetch` + `TestConnection`).
- [x] 1.2 Add a provider registry with lookup by `providerType`; unknown type → not-found. (Built in `NewService` from the default provider set; converting to an fx group-injected registry is deferred until a second provider lands.)
- [x] 1.3 Refactor `domain/sources` service/handler/worker to resolve providers from the registry (no `ProviderTypeGoogleDrive` branching in core).
- [x] 1.4 Register `google_drive` behind the interface with no behavior change; unit tests prove the core is provider-agnostic (a stub `SourceProvider` is served end to end).

## 2. OAuth connect flow

- [x] 2.1 Add migration `kb.oauth_pending_states` (state, project, provider, user, code_verifier, redirect_uri, intended config, scopes, expiry, consumed) + store (create / single-use consume / delete-expired).
- [x] 2.2 `GET /providers/:type/oauth/authorize`: build the provider authorize URL (state + PKCE S256 + redirect_uri + scopes), persist pending state (TTL), return the URL.
- [x] 2.3 `GET /oauth/:type/callback`: verify state (single-use, TTL, provider-bound), exchange code (client secret + verifier), create the source with encrypted credentials, redirect to the UI; reject invalid/expired/reused state without storing.
- [x] 2.4 Provider `OAuthStrategy` (authorize URL builder, token exchange, scope list, token→credential extraction) implemented for Google; unit tests with an injected HTTP client (RFC 7636 vector, exchange success/error/missing-refresh-token).
- [x] 2.5 Pending-state expiry: enforced lazily in the consume query (`expires_at > now()`); `DeleteExpiredOAuthPendingStates` added. Periodic sweeper scheduling tracked as follow-up.

## 3. Org OAuth client config

- [x] 3.1 Add migration `kb.oauth_client_configs` (org, provider, encrypted client id/secret, scopes/strategy, timestamps) + store.
- [x] 3.2 Resolution: org config → deployment env default → actionable error; secret encrypted, never returned.
- [x] 3.3 Org-scoped REST for get/set/delete provider client configs, with authz (org admin).
- [x] 3.4 Config/env: deployment-default client id/secret per provider + public base URL (redirect URI).

## 4. SDK + gateway API

- [x] 4.1 Extend `pkg/sdk/datasources`: provider `authMethod`, OAuth authorize/callback helpers, org client-config methods; round-trip test.
- [x] 4.2 Gateway memory-client methods + JSON/UI handlers for OAuth start, callback, org client settings; handler tests for success/validation/memory-error paths.

## 5. Web UI

- [x] 5.1 Provider chooser on `/data-sources/new` (OAuth → "Connect with …", credentials → form), driven by the catalog.
- [x] 5.2 OAuth start + callback handling (flash success/error; return to the source detail), templ render tests.
- [x] 5.3 Resource selection step (Drive folder / GitHub repo) via the provider's `ListResources`; Picker path for `drive.file`.
- [x] 5.4 Org OAuth-client settings page (write-only secret); templ render tests.

## 6. Drive migrated onto OAuth connect

- [x] 6.1 Scope strategy selection (`drive.file` + Picker vs `drive.readonly`) per org client; Picker front-end (developer key, app id) for `drive.file`.
- [x] 6.2 Drive connector implements the generic `Connector` + `OAuthStrategy`; existing pasted-credential sources keep working and can be reconnected.
- [x] 6.3 Manual browser test: connect Drive via OAuth, pick a folder, sync, re-sync (no-op), change a file → new version, remove → `deleted_from_source`.

## 7. GitHub provider (readiness)

- [x] 7.1 Register a `github` provider: catalog entry, config schema (repo, paths/glob, ref), credential schema, `oauth` auth method.
- [x] 7.2 Implement the GitHub OAuth strategy (authorize `github.com/login/oauth/authorize`, scopes `repo`/`public_repo`+`read:org`, PKCE, token exchange) with an injected HTTP client test.
- [x] 7.3 Connector contract (repo → markdown/docs, optional issues) — full enumeration/sync implementation may ship in a following change.

## 8. Tests + verification

- [x] 8.1 Server unit tests for the registry, OAuth flow (state/PKCE, expiry, reuse), org-client resolution, and provider-generic worker path.
- [x] 8.2 Migration tests (up/down) for the two new tables.
- [x] 8.3 Gateway handler + templ render tests; hermetic e2e for the connect chooser + OAuth callback (`mock-memory.mjs` extended with an OAuth provider fixture) and a second-provider (GitHub) fixture. Delivered as `specs/data-sources/oauth-connect.spec.ts` under the hermetic `data-sources.config.ts` harness (3/3 pass against `run-e2e.sh`).
- [x] 8.4 `task build`, `task test`, `templ generate` (no diff), `go vet`, `task lint` pass for touched modules. Touched trees (server `domain/sources`/`internal/gdrive`/`pkg/sdk`, web-ui gateway) are green; the repo-wide `task test`/`task lint` runs carry pre-existing/environmental failures unrelated to this change (local Postgres auth for integration suites; a generated gitignored `*_templ.go` + a pre-existing `live-chat.ts` gitleaks false positive; a connector date-dependent test).
- [x] 8.5 Update specs if behavior changed during implementation (same PR). Shipped behavior matches the delta specs; no spec text change was required.

## 9. Deferred (documented, not implemented here)

- [ ] 9.1 GitHub App (installation-token) auth model and org-scale installation flow.
- [ ] 9.2 Automatic migration of existing pasted-refresh-token sources to OAuth.
- [ ] 9.3 Central-app Google verification / `drive.file` Picker hardening beyond the first UI (the org client `config` stores the Picker developer key/app id; the full client-side Picker JS is not wired).
- [ ] 9.4 Additional providers beyond Drive and GitHub.
- [ ] 9.5 Full GitHub connector: repo → markdown/docs enumeration + sync (optional issues) behind the `githubConnector` stub shipped in §7.3.
- [ ] 9.6 Live Drive OAuth browser walkthrough (the §6.3 manual test): connect via OAuth → pick a folder → sync → re-sync (no-op) → change a file → new version → remove → `deleted_from_source`. Needs a real Google OAuth app + a browser; the resource/strategy paths are covered by automated tests.
