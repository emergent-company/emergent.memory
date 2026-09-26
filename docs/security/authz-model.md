# Authorization model

This is the developer-facing reference for how authorization is *actually* enforced in the Go
server. It documents merged behaviour on `origin/main`, not a design. It is the durable answer to
the largest recurring defect class in this repo (~30 instances of wrong tier, client-supplied
identity, missing membership, child ownership, and MCP bypassing HTTP gates) — see
[`mcp-authz-class-decision.md`](./mcp-authz-class-decision.md) for the parity audit and decision
(merged via PR #1054, design in PR #991).

Two adjacent references, always read with this one:

- **`openspec/specs/scope-authority/spec.md`** — the normative spec for scope resolution and the
  tier grants. The `## Requirements` there are the contract; this file is the map of where the
  contract lives in code and how to extend it without re-opening a closed class.
- **`apps/server/pkg/auth/`** — the middleware and helpers. Every guard below is named here.

A note on vocabulary: there is **no `pkg/authz` package**. The typed
`RequireAuthority(resource, level)` / `DerivePrincipal` / `Authorize` abstraction is a design
(PR #991, `openspec/changes/authz-abstraction/`) that is **not yet merged**. Do not document it as
if it existed; when you write code today you use the helpers below.

---

## Authority tiers

Authorization is a **scope** vocabulary resolved at the service boundary. There is no single enum
type; the tiers are the role constants in `pkg/auth/scope_mapping.go:17-32` and the resolution
order in `resolveOIDCScopes` (`pkg/auth/scope_mapping.go:218-276`).

| Tier | Grant source | Result | Code |
|---|---|---|---|
| 0 | token-carried Memory scope (OIDC) | verbatim, terminal **only while** `MEMORY_OIDC_TRUST_TOKEN_SCOPES` is enabled | `scope_mapping.go:220-224` |
| 1 | `superadmin_full` | full scope catalogue, terminal | `scope_mapping.go:226-236`, `superadmin.go:21-40` |
| 2 | `org_admin` membership | `org:read`, `org:invite:create`, `org:project:create`, `org:project:delete` — **no** `project:*`/data/schema/agent scope | `scope_mapping.go:29-32`, `:355-375` |
| 3 | project membership role | viewer ⊂ user ⊂ admin (nested by construction) | `scope_mapping.go:52-57`, `:107-118` |
| 4 | app-owned default scope set | only when the user has no membership/entitlement | `scope_mapping.go:274-275` |
| 5 | none | fail closed | `scope_mapping.go:274-275` |

Canonical role strings (`scope_mapping.go:17-26`):

- `project_viewer` / `project_user` / `project_admin` — `kb.project_memberships.role`.
- `superadmin_full` / `superadmin_readonly` — `core.superadmins.role`. **Only `superadmin_full`
  is a scope tier**; `superadmin_readonly` receives nothing at the scope layer (its read surfaces
  are role-gated — issue #812 Q9).
- `org_admin` — `kb.organization_memberships.role`.

Any unrecognised role (a legacy `owner` row, a typo) is unmapped and `resolveOIDCScopes` fails
closed to an empty set — never the configured default (`scope_mapping.go:107-118`, `:251-259`; the
#736 decision B strict rule).

`superadmin_full` is **not a scope**; it is a role read from `core.superadmins` by the shared
`superadminRole` helper (`pkg/auth/superadmin.go:21-40`), which both the middleware
(`RequireSuperadminFull`, `:64-84`) and the handler layer (`IsSuperadminFull`, `:48-58`) consume.
An `admin:all` token minted by any `org_admin` can **never** satisfy it — issue #940 review.

---

## Rule 1 — identity/context is never derived from client input where a token binding exists

The project/org a request acts on is resolved server-side. `X-Project-ID`, `X-Org-ID`, a
`?project_id` query param, and a body `projectId` field are **inputs**, never authorization truth.

- **`RequireAuth`** (`pkg/auth/middleware.go:371-490`) resolves the org from the *declared
  project's owning org* via `kb.projects.organization_id` (`:412-440`). A `X-Org-ID` header that
  conflicts with the project's owning org is rejected **403** (`:429-431`); with no project context
  and a DB available, the header is discarded and the org is left empty (`:436-440`). The
  `:projectId` path param is only ever a scope *hint*, and only in standalone mode, and never
  authorization truth (`:401-410`, issues #811/#850).
- **`RequireProjectTokenScope`** (`middleware.go:515-571`) is the token-binding check: an `emt_*`
  token may only address the project it is bound to, else **403**. It is a *token* guard, not a
  membership guarantee — the name states scope only (issue #861).
- **`RequireProjectMember` / `authorizeProjectMember`** (`middleware.go:601-674`) is the membership
  check: a session or unbound account-token caller must belong to the addressed project's owning
  org (`kb.organization_memberships`), resolved server-side. A caller-supplied project id can never
  self-satisfy it (the #849/#850 class; issue #877).
- **`AuthorizeProject`** (`middleware.go:688-709`) and **`RequireProjectMembership`**
  (`pkg/auth/project_membership.go:32-78`) are the handler-layer counterparts for a project id
  sourced from a query param or body field that the route middleware cannot see (issue #913).
- Domain helpers do the same by construction: `provider.assertCallerOwnsProject`
  (`domain/provider/access_control.go:53-76`) and `skills.requireProjectMember` resolve the owning
  org from `kb.projects` and the caller from the authenticated user.

Consequence: if a new route reads a project/org from a header, query param, or body field and acts
on it **without** one of the above checks, that is a finding. (The `GetProjectID` helper,
`middleware.go:75-92`, prefers the API-token-bound project over the header for exactly this
reason.)

## Rule 2 — every route declares its guard; `RequireAuth`-only on project-scoped data is a finding

A route whose whole guard is `RequireAuth()` but whose handler touches project- or org-scoped data
is under-protected. This is the #940 class: `/api/embeddings` control group was gated by
`RequireAuth` alone with no project/admin membership. On project-scoped data, the correct guard
chain is `RequireAuth` → `RequireProjectTokenScope` → `RequireProjectMember` (or an equivalent
handler-level `AuthorizeProject` / `RequireProjectMembership` / `assertCallerOwnsProject`).

When writing or reviewing a route, answer for each of the project-id sources the middleware pair
does *not* inspect (`?project_id`, a body field) whether the handler calls `AuthorizeProject` or
`RequireProjectMembership` before acting (issue #913).

## Rule 3 — one authorization decision per concept, shared across entrypoints

The same resource family must be authorized by **one** helper, called from both the REST handler
and the MCP tool. Re-implementing the check per entrypoint is how the two drift (the systemic
#1041 class: MCP tools calling the service layer directly, bypassing the HTTP handler gate).

The merged, working precedents — copy one of these:

| Concept | Shared helper | Both entrypoints | Fixed by |
|---|---|---|---|
| skill by-id read/write | `skills.Repository.AuthorizeSkillAccess` / `AuthorizeSkillWrite` (`domain/skills/store.go:256,298`) | REST `GetSkill` + MCP `skill-get/update/delete` | #1040 |
| global blueprint write | `blueprints.Service.AuthorizeGlobalBlueprintWrite` (`domain/blueprints/service.go:91`) | REST handler (4 sites) + MCP `blueprint-create/publish/new-version` | #1041 / #1060 |
| session-history access | `sessiontodos.SessionAccessibleQuery` (`domain/sessiontodos/repository.go:42`) | REST conversation history + MCP `session-get-messages` | #1056 |
| platform-admin authority | `auth.superadminRole` (`pkg/auth/superadmin.go:21`) | middleware `RequireSuperadminFull` + handler `IsSuperadminFull` | #940 |

The pattern is: **move the check into the service/store boundary**, so the two entrypoints call the
same function and cannot drift. Entrypoint-level checks do not close the Shape-A bypass (see the
decision doc §1.3, "blueprint-new-version" slipped past a scope-based audit because the missing
check was *inside* the service).

## Rule 4 — in-process vs transport are different trust markers

The MCP surface has three HTTP transports (`domain/mcp/handler.go`, `sse_handler.go`,
`streamable_http_handler.go`) and one in-process dispatch path, `mcp.Service.ExecuteTool`
(`domain/mcp/service.go:1929-2017`), reached by the ADK ToolPool during an agent run.

- **Transports** enforce the per-tool gate *before* dispatch — `AgentOnly` and arbitrary
  `RequiredScope` — then mark the context `TransportEnforced`
  (`handler.go:322-395`, `sse_handler.go:343-392`, `streamable_http_handler.go:585-647`).
- **`ExecuteTool`** cannot see token scopes (its principal is an agent run), so it enforces a
  different, coarser bar: the share allowlist, `SuperadminOnly` (`service.go:1959-1967`), the
  sensitive in-process admin set (`:1975-1983`), and — for *untrusted, non-transport* runs only —
  `AgentOnly` and the literal `"admin"` scope (`:2009-2017`). The in-process trust primitive is
  `kb.agent_runs.trusted_internal`: `true` for trusted/internal surfaces, `false` for webhook/A2A/
  agentcompat/public-share (issue #994).

This is the distinction that matters:

- **Transport-enforced trust** (`TransportEnforcedFromContext`) means an HTTP transport already did
  the fine-grained per-tool check; the in-process path may defer to it.
- **Genuinely-internal trust** (`TrustedInternalFromContext`) is a *different* marker: it does **not**
  carry arbitrary `RequiredScope` authority. An untrusted run that reaches `ExecuteTool` in-process
  bypasses per-tool scope enforcement for every scoped tool it is allowlisted to call — the live
  exposure documented in the decision doc §1.3 (mechanism 7), which the un-merged `pkg/authz`
  `AuthorizeTool` work is intended to close.

Do not confuse the two markers, and do not treat `TrustedInternal` as "already authorized".

---

## Regression guard

The parity between the MCP tool catalogue and the HTTP-equivalent authority is the thing that keeps
drifting. The proven guard is an **external `mcp_test` package** that imports both `mcp` and the
domain package, so it can compare the two sides (precedent: `agent_only_parity_test.go`,
`TestAgentOnlyParityHTTPInProcess`). A test inside `package mcp` cannot import the domains it
depends on. See the decision doc §2 Option B for the full mechanism list and its honest limits
(a table comparison does **not** catch a tool that declares a correct-looking scope but writes
globally — that is a service-layer check, Rule 3).
