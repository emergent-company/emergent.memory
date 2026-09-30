## Why

Memory's fine-grained authorization is currently split across **two authorities**. Zitadel is not just an identity provider: a Memory scope name carried on an OIDC token is honoured verbatim by `filterMemoryScopes` (`apps/server/pkg/auth/scope_mapping.go:112`), and the `oidc-scope-mapping` spec pins this as *"explicit Memory scopes are authoritative"*. At the same time the app maintains its own full authority plane — `roleToScopes` from `kb.project_memberships`, the operator default set, the userinfo all-grant, `core.api_tokens.scopes`, and `core.superadmins`.

The original intent was narrower: Zitadel should carry only a **coarse, high-level signal** (at most "this identity is a platform superadmin"), while the app owns **everything fine-grained**. Today it does not. The consequences:

- **Two places to change a scope, neither authoritative.** An operator can grant `data:write` by editing Zitadel token scopes *or* by editing a project membership, and the two paths do not agree on precedence in a documented way. Token scopes silently override role derivation (first-match-wins), so an IdP misconfiguration can grant or revoke access independent of the app's membership model.
- **A credential-plane attack surface.** Whoever can mint or edit a Zitadel scope for a client can mint a Memory grant, bypassing `kb.project_memberships` entirely. This is duplication of authority, not just duplication of configuration.
- **An app-owned surface wearing IdP names.** `ZITADEL_OIDC_DEFAULT_SCOPES` (`apps/server/internal/config/config.go:203`) and `ZITADEL_USERINFO_GRANT_ALL_SCOPES` (default `true`, `config.go:211`) are Memory policy knobs named after a third-party product, obscuring that the app is the decision-maker.
- **A load-bearing permissive default.** The userinfo all-grant (`middleware.go:638`) hands the full scope catalogue to any authenticated user whenever introspection is unconfigured. It is a pilot posture that has become a quiet default rather than a loud, time-boxed exception.
- **A one-off entitlement check.** Exactly one *shared* authorization decision reads organization membership — `apitoken.CanGrantAdminAll` (`domain/apitoken/repository.go:270`) — while a route-local project-transfer check (`domain/projects/service.go:409-414`) reads it with its own query. The tier between `project_admin` and `superadmin` is therefore effectively unmodelled.
- **Vocabulary drift.** The app writes organization memberships as `org_admin` (`domain/orgs/repository.go:153`) and checks `org_admin` (`apitoken/repository.go:276`, `domain/projects/service.go:413`), but `openspec/specs/project-viewer-role/spec.md:10` (and migration 00165) assert that `owner` is the valid `kb.organization_memberships` role — and `standalone/bootstrap.go:143` actually writes `owner`. (The `domain/invites/service.go:175` entry is an invite-role allow-list for a different table, `kb.org_memberships`, not an organization-membership write.) The standalone bootstrap org creator therefore never satisfies the org-admin check — a latent gap, though standalone requests already carry `GetAllScopes()` (see `design.md` D5).

This change is the **design** for collapsing that to one authority. It writes no implementation; the operator wants to read the design before anything is built.

## What Changes

Target principle: **Zitadel authenticates; the app authorizes.** No third authority. An IdP-side coarse signal, if an operator wants one at all (e.g. to bootstrap the first superadmin over SSO), maps to **one** app-side superadmin grant — never to fine-grained Memory scopes.

Five coordinated changes:

1. **Token-carried Memory scopes become opt-in (target default off).** `filterMemoryScopes`' trust path is placed behind an app-owned config flag. When off, Memory scope names present on the token are ignored and resolution falls through to app-derived entitlements. The flag is **introduced** defaulting `true` so the first release changes no behaviour, then flips to `false`. This is the change that removes the duplication.
2. **App-owned naming.** Introduce app-owned names for the two existing knobs — `MEMORY_OIDC_DEFAULT_SCOPES` and `MEMORY_USERINFO_GRANT_ALL_SCOPES` — plus the new flag. Keep each `ZITADEL_*` name as a deprecated alias for one release and emit a startup warning on use.
3. **Make the permissive default loud, then remove it.** While `UserinfoGrantAllScopes && !introspectionConfigured`, emit a startup warning and expose a dedicated health object; then delete the knob once introspection is the norm.
4. **Introduce app-side entitlement tiers.** Specify `superadmin → org_admin → project role` with **explicit per-tier grants**: superadmin = full catalogue (terminal), `org_admin` = a bounded organization-administration set, project membership = the #803 role sets. These tiers are new — `core.superadmins` and `kb.organization_memberships` are not scope sources today — and they replace the all-grant.
5. **Vocabulary reconciliation.** `org_admin` is authoritative for `kb.organization_memberships`; correct the `owner` claim in specs and normalise the `owner` rows.

Target resolution order: an API token's scopes are the effective scopes for machine callers; for OIDC sessions, token-carried scopes (opt-in, terminal) → app entitlement tiers (superadmin / `org_admin` / project membership for the declared project) → app-owned default → **empty** (fail closed). The design specifies each tier's exact grant; see `design.md` D4.

## Capabilities

### New Capabilities

- `scope-authority`: the single-authority principle; the target scope resolution order; app-owned configuration vocabulary; opt-in token-carried scopes; the loud, time-boxed permissive default; the organization entitlement tier; and the canonical organization membership role.

### Modified Capabilities

- `oidc-scope-mapping`: token-carried Memory scopes are honoured only when the trust flag is enabled; the default scope set and the all-or-nothing userinfo grant are renamed to app-owned env vars with deprecated `ZITADEL_*` aliases; the resolution order gains the organization entitlement tier and fails closed when token trust is off.
- `project-viewer-role`: the canonical `kb.organization_memberships` role is corrected from `owner` to `org_admin` while `kb.project_memberships` keeps its three canonical roles.

## Impact

- **`apps/server/pkg/auth/scope_mapping.go`** — token-trust flag gate on `filterMemoryScopes`; app-owned default-set accessor; org entitlement resolution seam.
- **`apps/server/pkg/auth/middleware.go`** — `finalizeOIDCUser` all-grant gate. `RequireScopes`/`RequireAPITokenScopes` already consume `user.Scopes` and are not expected to change, but both enforcement points SHALL be audited against the new tiers during implementation; the new org-scoped entitlement check is additive.
- **`apps/server/internal/config/config.go`** — new app-owned env names; deprecated `ZITADEL_*` aliases; all-grant default preserved then reconsidered.
- **`apps/server/domain/apitoken/repository.go`** — `CanGrantAdminAll` becomes the first consumer of the shared org entitlement rather than a bespoke query.
- **`apps/server/domain/health/`** — a dedicated `scope_authority` health object (the existing `Check` type is `{Status, Message}` and cannot carry the booleans).
- **`apps/server/domain/standalone/bootstrap.go`** + a Goose migration — normalize `owner` org memberships to `org_admin`.
- **Specs** — `openspec/specs/oidc-scope-mapping/spec.md`, `openspec/specs/project-viewer-role/spec.md` (on archive).

## Out of Scope

- **No implementation.** This change ships proposal + design + tasks + delta specs only. Any `apps/**` change is a separate follow-up change.
- **Scopes stay global per request.** This is *not* per-project scope isolation; the suite is not being re-architected into per-project permission sets.
- **Umbrella expansion semantics are frozen.** `schema:write ⇒ schema:migrate`, `agents:write ⇒ chat:admin`, and the `data:write` implications were decided and pinned by tests in #803; this design does not relitigate them.
- **#736 item 3 (live-Zitadel e2e)** is itself out of scope — but note it becomes **materially smaller** once Zitadel is reduced to identity (see `design.md`).
