# Tasks

<!-- openspec:archive-hold: predecessor changes add-provider-oauth-connect and add-data-sources are
still unarchived on main, so the main specs this change MODIFIES (data-source-providers,
web-data-sources) do not exist yet; archiving now would be refused. Archive after those land. -->

> Design/spec stage. Implementation ships on this same branch/PR (spec + implementation together);
> no task is complete until its code + tests land.

## 1. Project-scoped OAuth client config (server)

- [ ] 1.1 Add migration for project-scoped OAuth client storage (project, provider, encrypted client id/secret, scopes, strategy, provider config, timestamps) reusing the existing org-table shape and encryption path.
- [ ] 1.2 Extend the store with project-scoped get/set/delete; secret encrypted at rest, never returned.
- [ ] 1.3 Resolution precedence at connect time: project config → org config → deployment env default → actionable error.
- [ ] 1.4 Project-scoped REST: `GET/PUT/DELETE /api/projects/:projectId/data-source-integrations/oauth-clients[/:providerType]`, authz `project_admin` of that project only.
- [ ] 1.5 Extend the OAuth-client DTO with deployment-wide `redirectUri` (provider callback) and `javascriptOrigin`; org and project endpoints both return them.

## 2. Drive OAuth-only

- [ ] 2.1 Drive's `AuthMethods` becomes `[oauth]`; remove the Drive `credentials` (paste-refresh-token) connect path.
- [ ] 2.2 Existing pasted-refresh-token sources keep syncing (stored refresh tokens still honoured).
- [ ] 2.3 Update catalog/auth-method tests so Drive never advertises a credential form.

## 3. Config/env

- [ ] 3.1 Split `SOURCES_OAUTH_LANDING_URL` (UI landing origin) from `SOURCES_OAUTH_REDIRECT_BASE_URL`; landing uses the new var with the redirect base as fallback.
- [ ] 3.2 Derive the DTO `redirectUri` and `javascriptOrigin` from the resolved deployment config.

## 4. SDK + gateway API

- [ ] 4.1 Extend `pkg/sdk/datasources` with project OAuth-client get/set/delete methods + DTO fields; round-trip test.
- [ ] 4.2 Gateway memory-client methods + JSON/UI handlers for project OAuth-client settings; handler tests for success/validation/authz/memory-error paths.

## 5. Web UI

- [ ] 5.1 Add a scope-agnostic OAuth-client card used by both org and project settings pages, with a Google Cloud Console setup guide and the shared `redirectUri`/`javascriptOrigin` display.
- [ ] 5.2 Add a project settings "Data source OAuth clients" page (`/settings/oauth-clients`), `project_admin`-gated; reached from the context-aware missing-client alert.
- [ ] 5.3 Reuse the shared card on the existing org OAuth-client settings page.
- [ ] 5.4 Make the "no OAuth client configured" alert context-aware: link to project settings when the caller can administer the project, else org settings.
- [ ] 5.5 Remove the Drive paste-refresh-token form from the connect surface.

## 6. Tests + verification

- [ ] 6.1 Server unit tests: project client CRUD + authz, resolution precedence, Drive OAuth-only auth method, DTO fields.
- [ ] 6.2 Migration up/down test for the project-scoped table.
- [ ] 6.3 Gateway handler + templ render tests; hermetic e2e for the project OAuth-client settings page and the context-aware missing-client alert.
- [ ] 6.4 `task build`, `task test`, `templ generate`, `task lint` pass for touched trees.

## 7. Docs

- [ ] 7.1 Update `docs/site/user-guide/datasources.md`: Drive is OAuth-only, Google Cloud Console app setup at org and project level, resolution precedence.
- [ ] 7.2 Update spec if behavior changed during implementation (same PR).
