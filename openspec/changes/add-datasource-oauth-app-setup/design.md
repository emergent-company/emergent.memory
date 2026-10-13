## Context

`add-provider-oauth-connect` shipped the provider-generic OAuth connect flow and made the OAuth app
(`client_id`/`client_secret`) configurable **per organization** (`kb.oauth_client_configs`), resolved
as org config → deployment env default → error. Google Drive connects via OAuth but still advertises
a `credentials` fallback (the legacy "paste a refresh token" form). This change adds **project-scoped**
OAuth app setup and retires Drive's paste-refresh-token path.

## Goals / Non-Goals

**Goals**
- A project team can configure its own OAuth app (its own Google Cloud project, consent, quotas,
  billing) without an org-wide decision.
- Resolution is deterministic: project → org → deployment env default → actionable error.
- Authority is minimal: `org_admin` for org endpoints, `project_admin` for project endpoints.
- Drive is OAuth-only; the paste-refresh-token path is removed while existing pasted sources keep
  syncing.

**Non-Goals**
- Per-source (integration-level) OAuth app config — the granularity stays at project/org.
- Per-field override: a project override replaces the whole client, not individual fields.
- Migrating existing pasted-token sources to OAuth automatically — they keep working and can be
  reconnected.
- A general OAuth broker for arbitrary third parties.

## Decisions

### D1 — Resolution precedence: project → org → deployment env default → error

When a connect starts for a provider, the client is resolved in this order and the first match wins:

1. **Project config** — the active project's own client for that provider.
2. **Org config** — the project's org client for that provider.
3. **Deployment env default** — e.g. `SOURCES_OAUTH_GOOGLE_CLIENT_ID/SECRET`.
4. **Actionable error** — none present; the connect fails with a message naming the provider and the
   remediation location.

**Why:** a more specific scope should win so a project team can diverge from its org, while the
org-level and deployment defaults remain zero-setup paths for everyone else. **Alternative rejected:**
org-wins-always — defeats the point of project-level BYO.

### D2 — Authority: `project_admin` for project endpoints, `org_admin` unchanged

The new project OAuth-client routes require `project_admin` **of that project only**; the existing org
routes keep their `org_admin` requirement. No role escalation is introduced.

**Why:** the OAuth app is a security-sensitive credential; the narrowest role that can administer the
scope is correct. A project admin of project A MUST NOT read or set another project's client (that
scope belongs to the org or the other project).

### D3 — Override granularity: whole client per project

A project override is the **entire** client (`client_id`, `client_secret`, `scopes`, `strategy`,
provider `config`). Setting one replaces the previous; there is no per-field merge with the org or
deployment default.

**Why:** partial merges (e.g. "inherit org secret but override scopes") are hard to reason about and
to render in a settings UI; a whole-client override is simpler to validate and to display. The secret
remains write-only — an update that omits the secret preserves the stored one, matching the org
endpoint's existing semantics.

### D4 — Extend the existing table, don't add a second credential model

Project-scoped storage reuses the existing `kb.oauth_client_configs` shape (encrypted client
id/secret, scopes, strategy, provider config) with a project scope column, rather than introducing a
separate `project_oauth_client_configs` table.

**Why:** one encryption path, one resolution code path, and one DTO to maintain. The store gains
project-scoped get/set/delete mirroring the org-scoped methods. **Alternative rejected:** a separate
table + separate DTO — duplicates encryption/resolution logic and doubles the settings UI surface.

### D5 — Drive is OAuth-only; pasted sources keep syncing

Drive's `AuthMethods` becomes `[oauth]`. The `credentials` connect path (paste a refresh token) is
removed from the catalog and the UI. Existing Drive sources whose credential is a stored refresh
token continue to sync — the stored token is still honoured server-side; only *new* paste-refresh-token
connects are removed.

**Why:** the paste path leaks a long-lived secret through the UI and was always a stopgap ahead of the
OAuth flow. Retiring it for Drive removes that footgun without breaking existing sources.
**Alternative rejected:** keep `credentials` for Drive — leaves two connect paths to document and test,
and keeps the secret-paste UX alive.

### D6 — Scope-agnostic OAuth-client card shared by org and project pages

One card component renders the OAuth app setup (client id, secret, scopes, strategy) and is used
verbatim on the org and project settings pages. It carries a Google Cloud Console setup guide and
displays the deployment-wide `redirectUri` and `javascriptOrigin` identically on both pages.

**Why:** the settings are structurally identical whether scoped to org or project; a single card avoids
divergent UI and divergent validation. The card is scope-agnostic — it receives its target scope
(org vs project) from the page that embeds it. **Alternative rejected:** two near-duplicate cards —
drift risk with no benefit.

### D7 — Split `SOURCES_OAUTH_LANDING_URL` from `SOURCES_OAUTH_REDIRECT_BASE_URL`

The post-callback browser landing origin (where the UI lives) is split into its own
`SOURCES_OAUTH_LANDING_URL`, leaving `SOURCES_OAUTH_REDIRECT_BASE_URL` as the API origin that builds the
OAuth redirect URI. Landing falls back to the redirect base when the landing var is unset.

**Why:** the API origin (where the callback is registered) and the UI origin (where the browser lands)
can differ — e.g. `https://api.dev…` vs `https://admin.dev…` — so coupling them forces the wrong
landing host or the wrong redirect URI. The `redirectUri`/`javascriptOrigin` DTO fields are derived
from this split.

## Risks / Trade-offs

- **Project override of a secret is high-value to attackers** — mitigated by `project_admin`-only
  authority, write-only secret, and encryption at rest (same protections as the org path).
- **Precedence ambiguity** (does a project override silence the org app for *all* members of that
  project?) — yes by design: the project app applies to the whole project's connects; documented in
  the user guide.
- **Removing Drive's credentials path could strand a user mid-migration** — mitigated: existing pasted
  sources keep syncing; only new pasted connects are removed; OAuth reconnect is the documented path.
- **Redirect-URI misconfig** remains the #1 OAuth support cost — the card's console guide and the
  `redirectUri`/`javascriptOrigin` display surface the exact values to register.

## Migration Plan

1. Add the project-scoped storage column/table + store methods (no behavior change).
2. Add project OAuth-client REST + SDK + gateway methods with `project_admin` authz.
3. Add resolution precedence (project → org → env default → error) at connect time.
4. Flip Drive's `AuthMethods` to `[oauth]`; remove the paste-refresh-token connect path.
5. Split `SOURCES_OAUTH_LANDING_URL`; derive the DTO `redirectUri`/`javascriptOrigin`.
6. Add the shared scope-agnostic card + project settings page; reuse on org page; make
   the missing-client alert context-aware (the project page's entry point, matching the org page).
7. Update `docs/site/user-guide/datasources.md`.

**Rollback:** the project endpoints, precedence layer, and landing-URL split are additive; reverting
the change returns to org-only config + env default, and re-enables Drive's paste path (the removed
code is recoverable from git history). No existing pasted source is modified.

## Open Questions

- Should a `project_admin` be able to *clear* a project override to fall back to the org app? Yes —
  DELETE on the project client restores org/env resolution; nothing is left in a broken state.
- Should the project override also appear (read-only) to `org_admin`? Deferred — the org page shows
  org config only; project overrides are visible on the project page.
