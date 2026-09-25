# Design — authorization enforcement abstraction

## Context

The `scope-authority` change (#812) established **one authority** for fine-grained scopes: the app derives scopes from `core.api_tokens`, `core.superadmins`, `kb.organization_memberships`, `kb.project_memberships`, and an app-owned default, with Zitadel reduced to identity plus at most one coarse superadmin signal. This change is the **enforcement** counterpart: given that the app has resolved *what* a caller holds, it defines *how* that is checked, so the eight recurring mechanisms stop recurring.

The audit evidence (grounded, all on disk at the audited sha):

- **Mechanism 3.** `auth.RequireAuth` reads `user.ProjectID` from the client `X-Project-ID` header (`middleware.go:381`), and `auth.GetProjectID` prefers `APITokenProjectID` then falls back to that client header (`middleware.go:75-92`). The org is derived server-side (`middleware.go:389-440`), but the *project* is a client-controlled field used by every downstream guard.
- **Mechanism 4.** Route groups exist that carry only `RequireAuth` (e.g. `domain/apitoken/routes.go:53-56`, `/api/tokens` POST → `CreateAccountToken`).
- **Mechanism 2/6.** `domain/apitoken.Service.create` gates only `admin:all` via `checkAdminAllGrant` (`service.go:92,260`); the bare `admin` scope is a valid token scope (`entity.go:103`) mintable by any authenticated caller, and `CreateAccountToken` (`service.go:427`) is reached with only `RequireAuth`.
- **Mechanism 7.** `mcp.Service.ExecuteTool` (`service.go:1796`) enforces only the share-instance allowlist (`InstanceDeniesTool`) and hidden built-ins; the per-tool `RequiredScope`/`AgentOnly` check lives **only** in the three HTTP transport handlers (`handler.go:308-310`, `sse_handler.go:339`, `streamable_http_handler.go:569`), not in the in-process path used by the ADK ToolPool during agent runs.
- **Mechanism 5.** There is no child-ownership primitive: handlers that address a child by bare id (document, entity, agent, branch, schema, token, share instance) each invent their own parent lookup or none.
- **Mechanism 1.** `RequireScopes`/`RequireAPITokenScopes` (`middleware.go:779-853`) assert a scope only; nothing ties the scope to the *resource* the route actually touches.

Non-goals, stated first so they are not mistaken for scope: this is a **design** change (no implementation); it does not delete any existing guard; it does not change scope resolution, the umbrella-expansion relation, or the pinned role scope sets from #803; it is not a framework — the pieces are small and composable, each adoptable independently.

## The model (A)

### Authority tiers

Four tiers. `Level` is a total order of **administrative reach**, but note the org/project/child tiers govern **disjoint resource families** (as `scope-authority` already specifies for org vs project scopes); only `platform` is a true superset.

| Tier | Level | Who holds it | Governs |
|---|---|---|---|
| platform | `LevelPlatform` | active `superadmin_full` (`core.superadmins`, `revoked_at IS NULL AND role='superadmin_full'`) | everything, terminal |
| org | `LevelOrg` | `org_admin` of a specific org (`kb.organization_memberships`) | org-family resources of **that** org: members, invites, projects CRUD, org settings |
| project | `LevelProject` | a membership role in a specific project (`kb.project_memberships`) | project-family + child resources of **that** project |
| child | `LevelChild` | ownership of a specific child, resolved to its owning project | a single child resource under a project the caller is a member of |

### Principal kinds

| Principal | Auth plane | Derivation |
|---|---|---|
| OAuth session | OIDC (`validateToken` → `finalizeOIDCUser`) | `AuthUser.ID`, scopes resolved from entitlements |
| Account API token | `emt_*`, `project_id IS NULL`, `user_id` set | `AuthUser` with `APITokenProjectID == ""`, owning user resolved |
| Project-bound token | `emt_*`, `project_id` set | `APITokenProjectID` binds it to one project |
| Agent | in-process run (`TrustedInternal`, #981) | run row's project + `TrustedInternal` marker |
| Public share / anonymous | share key / `share:agent-chat` marker | share link → agent → project, allowlist-scoped |

### Resource kinds

`platform` (superadmin surfaces, MCP registry, sandbox images, extraction admin), `organization` (members, invites, project CRUD, org settings), `project` (data, schema, agents, tokens, chat), `child` (document, entity, agent, branch, schema instance, token, share instance, member, webhook, device credential — anything owned by a project or a child of it).

### The invariant

> An authority grant is scoped to a resource family and a specific org/project. It SHALL be exercised only over a resource in that family and within that scope. The required level for a resource equals its tier. A client SHALL NEVER promote its own identity: the caller's org/project is derived from credential + route, and any client-supplied project/org id is a **hint** validated against the derived identity, never an authority input.

Two corollaries, both load-bearing:

- **Org and project tiers are disjoint for data.** `org_admin` governs org administration and does **not** grant project-data access (matches `scope-authority`'s "no `project:*` scope" rule). A pure `org_admin` calling a project-scoped entrypoint is denied unless it also holds project membership.
- **No grant crosses its scope.** An `org_admin` of org A cannot administer org B; a `project_admin` of P cannot read P's sibling project. Child ownership is resolved *through* the project, so a bare child id can never self-satisfy.

## The abstractions (B)

All sketches are grounded in the types that exist today (`auth.AuthUser`, `auth.ExpandScopes`, `auth.CanGrantAdminAll`, the `Middleware` guard methods, `mcp.Service.ExecuteTool`, `mcp.ToolDefinition`). New code lives in `pkg/authz`, a sibling of `pkg/auth` with **no** DB and **no** Echo dependency in its core: it operates on `Principal` + `Resource` + injected resolvers, so `pkg/auth` and every domain package can import it without a cycle. A thin `pkg/authz/echo` adapter holds the Echo-specific glue.

### B1 — Typed authority/resource vocabulary

```go
package authz

type Level uint8

const (
    LevelPlatform Level = iota // superadmin_full (terminal superset)
    LevelOrg                    // org_admin of the resource's owning org
    LevelProject                // membership role in the resource's project
    LevelChild                  // ownership of a child, resolved via its project
)

type Kind string

const (
    KindPlatform     Kind = "platform"
    KindOrganization Kind = "organization"
    KindProject      Kind = "project"
    KindChild        Kind = "child"
)

// RequiredLevel is each kind's intrinsic authority floor. A registry entry may
// require MORE than the floor but never less (asserted at registration, B2).
func (k Kind) RequiredLevel() Level

// Resource is the thing being authorized: a family plus a concrete id whose
// OWNING tier is resolved server-side, never taken from client input.
type Resource struct {
    Kind Kind
    ID   string // concrete resource id
}

// Principal is the caller, DERIVED from credential + route (B4). Handlers never
// construct one from client input.
type Principal struct {
    UserID string
    Grants []Grant // one per resolved entitlement
}

// Grant is a single resolved entitlement. Scope narrows it to one org/project/
// child; empty for LevelPlatform.
type Grant struct {
    Level Level
    Scope string
}

// OwnerResolver resolves a resource's owning org/project server-side. Injected
// by pkg/auth (which owns the DB queries: dbProjectOrg, dbOrgMember, etc.) so
// authz stays free of DB and of any domain import.
type OwnerResolver interface {
    ProjectOrg(ctx context.Context, projectID string) (string, error)
    ChildProject(ctx context.Context, kind Kind, id string) (string, error)
}
```

Why this kills mechanisms 2 and 6: the level and kind are **values with a fixed vocabulary**, not ad-hoc strings. `Kind.RequiredLevel()` centralizes "a project entrypoint needs project-level authority", so a route can no longer gate project data with bare `admin` (mechanism 2) without the registration-time check rejecting the mismatch, and the vocabulary is the **only** place the tier→kind mapping exists (no lying copies — mechanism 6).

### B2 — Declarative surface registry, one enforcement point

```go
// SurfaceEntry is one entrypoint's declaration, registered once at package
// init (or route registration) beside the route it protects.
type SurfaceEntry struct {
    ID    string // e.g. "projects.tokens.create"
    Kind  Kind
    Level Level // must be >= Kind.RequiredLevel(); asserted at registration
    // Resolve extracts the concrete Resource from the request context. It may
    // read the :id path param, but MUST NOT read identity from headers/body.
    Resolve func(c echo.Context) (Resource, error)
}

type Registry struct { /* global; entries keyed by ID */ }

// Register adds an entry, failing loudly at registry time on:
//   - Level < Kind.RequiredLevel()   (mechanism 2)
//   - duplicate ID                     (mechanism 6)
//   - nil Resolve                      (mechanism 6)
func Register(e SurfaceEntry) error

// Enforce returns ONE middleware that applies the entry's declaration through
// the single Authorize path (B5), replacing a hand-assembled guard chain.
func (r *Registry) Enforce(entryID string) echo.MiddlewareFunc
```

Usage — the `apitoken` route group (today `routes.go:23-26`) becomes:

```go
// once, at init:
authz.Register(authz.SurfaceEntry{
    ID:    "projects.tokens.create",
    Kind:  authz.KindProject,
    Level: authz.LevelProject,
    Resolve: func(c echo.Context) (authz.Resource, error) {
        return authz.Resource{Kind: authz.KindProject, ID: c.Param("projectId")}, nil
    },
})

// route:
e.POST("/api/projects/:projectId/tokens", h.Create,
    authMiddleware.RequireAuth(),
    authz.Enforce("projects.tokens.create"),
)
```

The two `RequireProjectTokenScope` + `RequireProjectMember` calls are folded into `Enforce` — not deleted, but reached through one declarative seam. A route that forgets membership gets it for free; a route that needs `org` or `platform` says so in its entry and cannot accidentally slip down to `project`.

### B3 — `RequireAuthority` combinator with child-ownership resolution

```go
// RequireAuthority returns an error unless p holds `level` authority over res.
// It is the enforcement combinator everything reduces to. For KindChild it
// resolves the child's owning project (OwnerResolver.ChildProject) and then
// applies the project check, so a bare child id can never self-satisfy
// (mechanism 5). For KindOrganization/KindProject it resolves the owning
// org/project and requires a matching-scope grant. For KindPlatform it
// requires a platform grant. Returns a typed denial error (401 vs 403 vs 404)
// so the transport can render it consistently.
func RequireAuthority(ctx context.Context, p Principal, res Resource, level Level, r OwnerResolver) error
```

Ownership resolution is **in the combinator, not the handler**: to protect a child you declare `Kind: KindChild` and the resolver supplies the parent link once per kind (document→project, entity→project, agent→project, branch→project, schema→project, token→project, share-instance→project). Mechanism 5 is impossible to reintroduce because there is no path that checks a child id without first resolving its parent.

### B4 — Identity derivation by construction

```go
// DerivePrincipal builds a Principal from an already-authenticated AuthUser plus
// the route's declared resource. It is the ONLY place identity is turned into
// authority. X-Project-ID is demoted to a hint: it is used to pick a default
// project for scope resolution, but the project/org that become grants are
// resolved server-side and validated against the credential:
//   - project-bound token: project := token.project_id (header ignored/mismatch=403)
//   - OAuth session / account token: membership in the declared project's
//     owning org is checked before a project grant is issued
// The header can never mint a grant on its own (mechanism 3).
func DerivePrincipal(ctx context.Context, u *auth.AuthUser, res Resource, r OwnerResolver) (Principal, error)
```

This is not new *behavior* — `RequireProjectMember` already resolves the owning org server-side (`middleware.go:601-655`) and `RequireAuth` already derives org from the project (`middleware.go:389-440`). The abstraction **makes the safe path the only path**: a handler that wants a project id calls `DerivePrincipal` (which cannot be fed a client header) instead of reaching for `user.ProjectID` (which is a client header). The old `GetProjectID` helper remains for the transition but is marked deprecated, with a lint rule steering new code to `DerivePrincipal`.

### B5 — One transport-agnostic enforcement function

```go
// Authorize is the single choke point every transport calls:
//   - echo routes    → authz.Enforce (B2) → RequireAuthority (B3)
//   - in-process     → mcp.Service.ExecuteTool gains an Authorize call using the
//                      tool's declared (Kind, Level) before dispatch (worked
//                      example E2)
//   - SSE            → the SSE message handler calls the same per-tool check
//   - public share   → share resolution calls Authorize with the share-scoped
//                      allowlist as the principal's only grants
func Authorize(ctx context.Context, p Principal, res Resource, r OwnerResolver) error {
    return RequireAuthority(ctx, p, res, res.Kind.RequiredLevel(), r)
}
```

Today the per-tool `RequiredScope`/`AgentOnly` check is duplicated in three HTTP handlers and **absent** in-process (`service.go:1796`). `Authorize` collapses the four paths onto one, so a tool's authority is defined once and enforced identically everywhere (mechanism 7).

### B6 — Conformance test kit

```go
type CallerClass string

const (
    ClassUnauthenticated CallerClass = "unauthenticated"
    ClassProjectMember   CallerClass = "project-member"   // member of the addressed project
    ClassNonMember       CallerClass = "non-member"       // authenticated, different org
    ClassForeignProject  CallerClass = "foreign-project"  // member of a sibling project
    ClassBareScopeToken  CallerClass = "bare-scope-token" // project token, marker scope, no membership
    ClassAccountToken    CallerClass = "account-token"    // project-unbound token w/ owning user
    ClassSuperadmin      CallerClass = "superadmin"
    ClassAgent           CallerClass = "agent"            // in-process run (TrustedInternal)
    ClassShare           CallerClass = "share"            // public share / anonymous key
)

type ConformanceRow struct {
    Class   CallerClass
    Want    Decision // Allow or Deny
    Expects HTTPStatus // 401/403/404 rendered form, for the HTTP adapter
}

// Conformance derives the caller-class matrix for an entry from its (Kind,
// Level) declaration, so every module gets the same 6–9-row test for free.
// Each row is executed against the REAL Authorize path, not a re-implemented
// check, so a divergence between the declaration and the enforcement is a
// failing test rather than a silent gap (mechanism 8).
func Conformance(entry SurfaceEntry) []ConformanceRow
```

A `TestConformance` helper runs the matrix: for `KindChild`, it adds the "foreign-project child" and "bare child id, no parent" rows; for `KindProject`, the "bare-scope token" and "account token of a non-member" rows; for `KindOrganization`, the "org_admin of a different org" row. The module author supplies the fixtures (one valid member, one foreign member, one token), and the kit does the rest.

### B7 — CI coverage guard

```go
// TestRegistryComplete walks the registry and fails the build when any entry
// lacks a declaration, a conformance test, or a resolvable owner. Run in CI as
// a plain `go test ./pkg/authz/...`. A companion static check (go/analysis) scans
// RegisterRoutes call sites and flags any handler that is not backed by a
// registered SurfaceEntry.
func TestRegistryComplete(t *testing.T) {
    for _, e := range registry.All() {
        if e.Kind == "" || e.Resolve == nil { t.Errorf("entry %s: incomplete declaration", e.ID) }
        if len(Conformance(e)) == 0 { t.Errorf("entry %s: no conformance matrix", e.ID) }
    }
}
```

Mechanism 8 is why 1–7 survived: guards went untested. The kit + guard make "registered but untested" a build failure, so a route cannot be added (or a guard removed) without an explicit, enumerated caller-class matrix.

## How each mechanism dies (C)

| # | Mechanism | Abstraction that prevents it |
|---|---|---|
| 1 | Scope-only gate on project-scoped data | `RequireAuthority` checks **level over a resource**, not a bare scope; `Kind` ties the check to the resource family |
| 2 | Wrong authority level (`admin` on project data, org role on platform tooling) | `Kind.RequiredLevel()` + registration-time `Level >= floor` assertion (B1/B2) |
| 3 | Client-supplied identity trusted | `DerivePrincipal` (B4) — `X-Project-ID` demoted to a validated hint; handlers cannot mint a grant from it |
| 4 | Missing membership check | `Enforce(entryID)` (B2) applies membership for project/child entries by construction; `RequireAuth` alone is no longer a complete guard |
| 5 | Child ownership unchecked | `RequireAuthority` resolves child→parent before checking (B3) |
| 6 | Dead/lying authz code or spec | single vocabulary + single registry (B1/B2); no parallel implication tables or lying copies |
| 7 | Transport inconsistency | `Authorize` (B5) is the one function HTTP, in-process, SSE, and share all call |
| 8 | Harness blindness | conformance kit (B6) + CI coverage guard (B7) |

## Incremental adoption (D)

Strangler, module-by-module, no big-bang. Each step is independently shippable and reversible; the new layer runs **beside** the old guards, which are removed only as each module is migrated and its conformance matrix is green.

**Highest-leverage first step.** Land `pkg/authz` (B1 + B3 + B4 + B5 core) and migrate the **MCP in-process `ExecuteTool`** path (E2) plus **one** project-scoped HTTP domain end-to-end as the reference pattern. This single step closes the two known live findings (mintable `admin`; unenforced in-process scopes) and produces the worked pattern every later module copies. It is small enough to review carefully and its blast radius is bounded.

**Order.**

1. `pkg/authz` vocabulary + `RequireAuthority` + `DerivePrincipal` + `Authorize` core (pure, unit-tested).
2. Wire `ExecuteTool` to `Authorize` using each tool's `(Kind, Level)` (E2) — closes mechanism 7 for the in-process path.
3. Migrate `apitoken` token mint (E1) — closes the mintable-`admin` hole via a level-based mint check.
4. Register + migrate one project-scoped HTTP domain (e.g. `projects`, `documents`) to `Enforce(entryID)`; prove the conformance kit there.
5. Roll out the kit + CI guard (`TestRegistryComplete`), turning on coverage for already-migrated modules.
6. Migrate remaining domains in batches (org-scoped, child-scoped, platform-scoped), each with its conformance matrix.
7. Deprecate `GetProjectID`/`user.ProjectID` header reads; remove old guards per-module once matrices are green.

**Intermediate states.** A partially-migrated codebase runs both layers: new modules declare entries and call `Authorize`; unmigrated modules keep their existing `Require*` chains. The conformance kit only asserts over registered entries, so unmigrated routes neither pass nor fail the guard — they simply remain on the old path until migrated. There is no intermediate state where a route is less protected than before.

**Cost/risk.** Step 1 is low-risk (additive package, no callers). Steps 2–3 are the risky ones because they touch enforcement of a live surface — each must ship with its conformance matrix proving no caller class regresses. Step 7 (removing old guards) is the long tail; it is safe because a guard can be removed only when the entry's matrix shows the same decision for every caller class.

**Non-goals.** No big-bang rewrite; no deletion of `Require*` primitives before a module is migrated; no framework/DSL — the pieces are plain Go; no change to scope resolution or `#812`'s sequenced §7 default-flip / §8 code-deletion (this layer makes those safe to land, but does not depend on them and does not wait for them).

**Must not block on #812 §7/§8.** The `authorization-enforcement` layer consumes the *already-resolved* scopes/entitlements. It works identically whether token-scope trust is on or off, and whether the all-grant exists or not. It therefore neither blocks nor waits on `#812`'s default flip (Release N+1) or code deletion (Release N+2); the two changes are orthogonal.

## Worked examples (E)

### E1 — The mintable bare `admin` scope (mechanism 2/3, audit-core F1)

**Today.** `domain/apitoken.Service.create` (`service.go:236`) and `CreateAccountToken` (`service.go:427`) validate scopes against `ValidApiTokenScopes` (which contains bare `admin`, `entity.go:103`) and gate only `admin:all` via `checkAdminAllGrant` (`service.go:92,260`). `/api/tokens` POST is behind only `RequireAuth` (`routes.go:53-56`). Result: any authenticated user mints an `admin`-scoped account token and reaches `admin`-gated platform surfaces (sandbox exec, MCP registry, sandbox images, extraction admin).

**How the model prevents it.** Token mint is itself an entrypoint with a **level**: minting a token whose scopes include a platform-tier scope requires `LevelPlatform` (or `LevelOrg` for `admin:all`, per #812's deliberate narrowing). Instead of a bespoke `if scope == "admin:all"` gate, the mint path declares, per scope, the level required to *grant* it, drawn from the same typed vocabulary:

```go
// The mint check becomes: for every requested scope, resolve scope → required
// level from a single registry; RequireAuthority(caller, Resource{KindPlatform},
// thatLevel). Bare "admin" maps to LevelPlatform; "admin:all" to LevelOrg-or-platform
// (preserving #812 semantics); project scopes to LevelProject (membership already
// checked by the route).
func (s *Service) checkMintAuthority(ctx context.Context, p authz.Principal, scopes []string, r authz.OwnerResolver) error
```

Bare `admin` now maps to `LevelPlatform`, so a non-superadmin is denied at the single level-check — the escalation is structurally closed, and the mapping from scope→level lives in exactly one place (no more "`admin` slipped past because the gate only looked at `admin:all`").

### E2 — Unenforced in-process `RequiredScope`/`AgentOnly` (mechanism 7, audit-mcp)

**Today.** `mcp.Service.ExecuteTool` (`service.go:1796`) checks only the share-instance allowlist (`InstanceDeniesTool`) and hidden built-ins. The `RequiredScope`/`AgentOnly` check is duplicated in the three HTTP transports (`handler.go:308-310`, `sse_handler.go:339`, `streamable_http_handler.go:569`) and **absent** from the in-process path used by the ADK ToolPool during an agent run.

**How the model prevents it.** Each `ToolDefinition` already carries `RequiredScope` and `AgentOnly` (`entity.go:335-340`). Give `ExecuteTool` an `Authorize` call driven by a tool→`(Kind, Level)` projection:

```go
func (s *Service) ExecuteTool(ctx context.Context, projectID, toolName string, args map[string]any) (*ToolResult, error) {
    if scope := InstanceScopeFromContext(ctx); scope != nil && InstanceDeniesTool(scope, toolName) {
        return nil, fmt.Errorf("tool not allowed by MCP share instance: %s", toolName)
    }
    def := s.GetToolByName(toolName)
    if def != nil {
        // Projection: AgentOnly → deny for non-agent principal; RequiredScope →
        // a project-scoped authority check. This is the SAME check the three
        // HTTP transports already do, now in the one place every transport
        // (including in-process) reaches.
        if err := authz.AuthorizeTool(ctx, p, def); err != nil {
            return nil, err
        }
    }
    // ... existing dispatch ...
}
```

The HTTP transports then delegate to the same `AuthorizeTool` (or simply stop re-implementing it and rely on `ExecuteTool`'s check), so there is exactly one enforcement of a tool's declared authority, exercised identically on legacy RPC, streamable HTTP, SSE, share, and in-process. Mechanism 7 is closed at the seam, not by adding a fourth copy.

## Open questions

1. **Grant-vs-scope dual representation.** `Principal.Grants` and the existing `AuthUser.Scopes []string` overlap. Does `DerivePrincipal` replace `Scopes` as the canonical authority carrier (with `Scopes` kept only for MCP tool-listing compatibility), or are the two kept in parallel for the transition? Recommend: `Grants` becomes canonical; `Scopes` is derived from `Grants` for the MCP list-time filter.
2. **Org tier non-inclusivity.** Is `org_admin` truly **not** allowed to read project data (disjoint families), or should the model encode a controlled "org_admin may read (but not write) member projects"? The `scope-authority` spec says disjoint; confirm before baking it into `RequireAuthority`.
3. **Share/agent as first-class principals.** The `share` and `agent` caller classes have no `AuthUser`; `DerivePrincipal` must accept a non-`AuthUser` source for them. Confirm the share allowlist and the `TrustedInternal` run row are the correct sole authority sources.
4. **Child resolver registry.** `OwnerResolver.ChildProject` needs one parent link per child kind. Enumerate the child kinds exhaustively now (document, entity, agent, branch, schema, token, share-instance, member, webhook, device) or grow the registry lazily with a "no resolver → child entries rejected at registration" guard.
5. **`admin` scope disposition.** Does bare `admin` disappear entirely (folded into `admin:all` + `LevelPlatform`), or is it retained as a distinct platform scope with its own level? The mint fix in E1 needs this decided.
6. **Static route-lint vs runtime registry.** The CI guard can be a runtime `go test` (registry completeness) plus an optional `go/analysis` pass over `RegisterRoutes`. Is the runtime registry sufficient, or is the AST pass required to catch handlers that never register at all?

## Relationship to #803 and #812

Orthogonal to #803 (which pins *what* project roles map to) and to #812 (which pins *who may define* scopes). This change pins *how* resolved authority is enforced. It consumes `#812`'s resolved entitlements and does not depend on `#812`'s §7 default-flip or §8 code-deletion; conversely it makes both safe to land, because a uniform, tested enforcement layer means a default flip or a deleted grant path fails loudly in a conformance matrix rather than silently in production.
