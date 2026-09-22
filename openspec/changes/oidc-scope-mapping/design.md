## Context

`Middleware.validateToken` (`apps/server/pkg/auth/middleware.go:541`) is the single OIDC entry point. It returns `*AuthUser` with a **global** `Scopes` slice; downstream route guards (`RequireScopes`), MCP `tools/list` filtering, and `tools/call` all read that slice. The slice is derived differently per validation path, which is the defect this change addresses.

Two existing facts drive the design:

1. **Project context is only known at request time.** `X-Project-ID` is read in `RequireAuth` after `authenticate` returns. OIDC access tokens carry no Memory project. The only project a request can be safely attributed to is the one the caller declared for *that* request.
2. **`pkg/auth` cannot import `domain/projects`** (domain packages depend on `pkg/auth`), so role constants and the viewer scope set are mirrored locally with a comment pointing at the source of truth.

## Goals / Non-Goals

**Goals**

- Introspection (`ZITADEL_CLIENT_JWT` configured) becomes a usable default without granting every scope to every user.
- A configurable default scope set lets an operator unblock a pilot without waiting for the full role→scope policy.
- No code path grants more scopes than today's introspection path unless the operator explicitly opts in.
- A single-user pilot's all-or-nothing behaviour is preserved behind explicit config.
- Role-string inconsistency (`owner` vs `project_admin`) is fixed.

**Non-Goals**

- Defining the exact `project_admin` / `project_user` scope sets (product decision — see Decisions).
- Per-project scope *isolation* inside a single AuthUser. Scopes remain global for the duration of a request; project-level authorisation is still enforced by handler-level membership checks. Role derivation is bound to the request's declared project, which is the strongest available binding without a per-project session model.

## Decisions

### D1 — Resolve at request time using the declared project, not a union

Resolution happens inside `validateToken`, which now receives the request's `X-Project-ID` value (passed down from `authenticate`). A union across all memberships was rejected: a user who is `project_admin` in project A and `project_viewer` in project B would receive the admin union and could then present `X-Project-ID: B` and operate on B with admin scopes. Binding to the declared project prevents this cross-project escalation.

### D2 — Only `project_viewer` is mapped (authoritative)

`ViewerReadOnlyScopes` is defined twice in code (`domain/projects/entity.go:87` and the API-token service) and is the only role whose scope set is authoritative. It is the only role mapped here. `project_admin` / `project_user` / legacy `owner` have no in-code scope set; guessing one would be a security policy decision with cross-cutting blast radius, so it is left to the operator via the default set (see Decisions Needed).

### D3 — Resolution order (first match wins, no union)

1. **Explicit Memory scopes** present in the token's raw scopes (intersection with the Memory scope vocabulary) → used verbatim. Token is authoritative.
2. **Mapped role** for the declared project (`project_viewer`) → `ViewerReadOnlyScopes`.
3. **Configurable default set** (`ZITADEL_OIDC_DEFAULT_SCOPES`) — used for any other role value (including unmapped/legacy roles) or no membership, i.e. whenever there is no explicit Memory grant.
4. **Empty** — default set unset, or role lookup error.

`GetAllScopes()` is never returned by the resolver.

### D4 — Fail-closed by construction

- Default `ZITADEL_OIDC_DEFAULT_SCOPES` is **empty**, so with no operator opt-in an OIDC session resolves to exactly today's introspection behaviour: no Memory scopes.
- A role-lookup error yields empty, never the default set (default is a *grant*, not a fallback for failure).
- `GetAllScopes()` is reachable only through the explicitly-gated userinfo grant (D5).

### D5 — userinfo all-grant gated on introspection being unconfigured

Effective all-grant condition:

```
introspectionConfigured = !DisableIntrospection && (ClientJWT != "" || ClientJWTPath != "")
oidcGrantAll            = UserinfoGrantAllScopes && !introspectionConfigured
```

- Flag default `true` ⇒ zero config migration for the single-user pilot; behaviour unchanged while introspection is not configured.
- The flag is ignored once introspection is configured, and a transient introspection *error* does not re-enable the all-grant (introspection was still configured). This prevents outage-driven privilege escalation.

### D6 — Cache raw claims only

Only raw OIDC claims are cached (`kb.auth_introspection_cache` via `cacheIntrospection`, and the `ZitadelService` internal cache). Derived scopes are re-resolved on every request, so a role change or a default-set change takes effect on the next request rather than after TTL expiry. The `scope` column keeps holding the raw OIDC scope string.

Cache entries also record the `auth_source` so the userinfo all-grant is applied consistently on a cache hit. Cache entries written before this change carry no `auth_source`; they are treated as identity-only introspection entries with **no scopes**, because the pre-change userinfo path cached the full scope catalogue and replaying it would silently widen access right after deploy. Re-resolution therefore runs for them too.

### D7 — Canonical project-membership role strings

`kb.project_memberships.role` is constrained only by convention. `standalone/bootstrap.go` wrote `owner`, which no role check accepts (`domain/extraction/project_embedding_handler.go:42` requires `project_admin`), producing a latent 403. `owner` is **not** a synonym for `project_admin`; the canonical project roles are `project_admin` | `project_user` | `project_viewer`. `owner` remains correct for `kb.organization_memberships`. The bootstrap write is corrected and existing rows are normalised by migration.

## Threat model implications

- **Privilege escalation via project header** — mitigated by D1 (no cross-project union).
- **Silent widening on deploy** — mitigated by D4 (empty default = status-quo scopes).
- **Widening during issuer outage** — mitigated by D5 (all-grant requires introspection to be *unconfigured*, not merely failing).
- **Unmapped/typo'd role granting write access** — an unmapped role receives no *role-derived* scopes; it receives the default set only, which is an explicit operator policy applied uniformly to OIDC users without an explicit grant. With the shipped default (empty) this is fail-closed.
- **Stale elevated scopes after demotion** — mitigated by D6 (derived scopes never cached).
- **Scope set remains global for the request** — a user with a mapped viewer role in the declared project is restricted to read-only for that request; account-level (no `X-Project-ID`) requests fall to the default set.

## Rollback / operational notes

- **No schema change for scopes** — scope resolution is code + env only. Reverting the deploy restores previous behaviour immediately.
- **The only DB migration is a data normalisation** (`UPDATE kb.project_memberships SET role='project_admin' WHERE role='owner'`), idempotent and reversible in spirit (re-running with `owner` is a no-op after normalisation). It touches only rows that violate the documented role set.
- **Enabling the new default path:** set `ZITADEL_CLIENT_JWT` (introspection) and, if users need scopes beyond viewer, set `ZITADEL_OIDC_DEFAULT_SCOPES`. Leaving the default empty is safe and equivalent to today's introspection path.
- **Pilot preservation:** leave introspection unconfigured and `ZITADEL_USERINFO_GRANT_ALL_SCOPES=true` (default) → unchanged behaviour.
- **Observability:** role-resolution failures and all-grant suppression are logged at warn/debug; the resolver never logs raw tokens.

## Decisions Needed (reported, not implemented)

1. **`project_admin` / `project_user` OIDC scope sets.** No authoritative list exists in code. Options:
   - (a) Extend the mapping with conservative lists (admin ⊇ user ⊇ viewer; never `admin*`, `mcp:admin`, `org:*`, `project:invite:create`, `account:*`). Recommended, but requires owner sign-off because `data:write`, `schema:write` (implies `schema:migrate`), and `agents:write` (implies `chat:admin`) each silently widen.
   - (b) Keep them unmapped and require operators to set `ZITADEL_OIDC_DEFAULT_SCOPES` (implemented here).
   - Recommendation: ship (b) now; adopt (a) in a follow-up once the owner confirms the per-role lists.
2. **Whether an unmapped role should be excluded from the default set** (stricter fail-closed) rather than receiving it. Implemented as "receives the default set" because the default set is the issue's requested mechanism for OIDC users without an explicit grant, and the pilot's own auto-provisioned user holds `project_admin` (currently unmapped). Out of the box (empty default) both readings are identical.
