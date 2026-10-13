## Why

The first OAuth iteration (`add-provider-oauth-connect`) made a data-source provider's OAuth app
(`client_id`/`client_secret`) configurable **per organization** only, with a deployment-env default as
the fallback. That leaves two gaps:

1. **No project-scoped app setup.** A project team that wants its own Google Cloud project (its own
   OAuth consent, quotas, and billing) has no way to configure its app without asking an
   `org_admin` — coupling an org-wide decision to a per-project need, and forcing every project in
   the org onto one app.
2. **Drive is not OAuth-only.** The provider catalog still advertises a `credentials` path for
   Google Drive, so the legacy "paste a refresh token" flow is reachable alongside OAuth — a
   confusing, secret-leaking UX that the OAuth connect flow was meant to retire.

This change closes both: OAuth app setup becomes configurable **per organization and per project**
(the UI's "data source OAuth app setup" UX), and Google Drive moves to **OAuth-only** (the
paste-refresh-token path is removed).

## What Changes

- **Project-scoped OAuth client config**: a project can hold its own whole-client override
  (`client_id`, `client_secret`, `scopes`, `strategy`, provider `config`), encrypted at rest and
  write-only, managed by `project_admin`.
- **Resolution precedence**: starting a connect resolves the client as **project config → org
  config → deployment env default → actionable error**.
- **Authority**: org endpoints require `org_admin`; the new project endpoints require `project_admin`
  of that project only.
- **New REST**: `/api/projects/:projectId/data-source-integrations/oauth-clients[/:providerType]`
  (GET/PUT/DELETE). Org endpoints are unchanged.
- **DTO**: the OAuth-client DTO gains a deployment-wide `redirectUri` (provider callback) and
  `javascriptOrigin`, shown identically on the org and project settings pages.
- **Drive OAuth-only**: Drive's `AuthMethods` becomes `[oauth]`; the paste-refresh-token path is
  removed. Existing pasted sources keep syncing — stored refresh tokens are still honoured.
- **Config split**: a new `SOURCES_OAUTH_LANDING_URL` (UI landing origin) is split from
  `SOURCES_OAUTH_REDIRECT_BASE_URL`.
- **UI**: a single scope-agnostic OAuth-client card is shared by the org and project settings pages,
  including a Google Cloud Console setup guide; the project OAuth-client settings page is reached
  from the context-aware "no OAuth client configured" alert (which points at project settings when
  the caller can administer the project, else org settings) — the same entry point as the org page.

## Capabilities

### Modified Capabilities

- `data-source-providers`: project-scoped OAuth client credentials alongside org-scoped, resolution
  precedence, per-project authority, and the Drive OAuth-only auth method.
- `web-data-sources`: the shared scope-agnostic OAuth-client card (org + project), the project
  settings page, the console setup guide, and the context-aware missing-client alert.
- `data-sources`: the Drive paste-refresh-token connect path is removed (OAuth-only).

## Impact

- `apps/server/domain/sources/**` — project-scoped OAuth client store + routes, resolution
  precedence, Drive auth-method change.
- `apps/server/pkg/sdk/datasources/**` — project OAuth-client endpoints, DTO `redirectUri` /
  `javascriptOrigin`.
- `apps/server/internal/config/**` — `SOURCES_OAUTH_LANDING_URL` split.
- `apps/server/migrations/**` — project-scoped OAuth client config storage.
- `apps/web-ui/gateway/**` (`datasources*.go`, `datasources.templ`, settings rail) + e2e.
- `docs/site/user-guide/datasources.md` — Drive OAuth-only + org/project Google app setup guide.
- Existing pasted-refresh-token Drive sources are unaffected (stored tokens still honoured).
