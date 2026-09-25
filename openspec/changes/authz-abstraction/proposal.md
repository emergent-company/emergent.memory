# authz-abstraction

## Why

Memory has produced a long, recurring stream of authorization defects, and they recur through a small set of mechanisms rather than as unrelated slips. Two completed audits (`/tmp/opencode/audit-core.md`, `/tmp/opencode/audit-mcp.md`) and the `scope-authority` work (#812) together identify eight mechanisms:

1. scope-only gate on project-scoped data (a token scope treated as sufficient authority);
2. wrong authority **level** (project data gated by bare `admin`; platform tooling gated by org roles);
3. client-supplied identity trusted (`X-Project-ID` header, body/query `projectId`) instead of derived server-side;
4. missing membership check (a route carries only `RequireAuth`);
5. child-resource ownership unchecked (bare child id, no parent-ownership predicate);
6. dead or lying authorization code / spec;
7. transport inconsistency (HTTP route vs in-process dispatch vs SSE vs public share enforced differently);
8. harness blindness (guards untested, tests skipped) — which let 1–7 survive.

The root cause is not "someone forgot a guard again". It is that **correct enforcement is the hard path**: each entrypoint hand-assembles a guard chain from string scopes, each transport re-implements the same per-tool check, identity is read back out of client headers in `RequireAuth`, child ownership has no primitive at all, and nothing forces a route to state what it protects or to test every caller class. Every one of the eight mechanisms is a predictable failure of a hand-rolled chain that the codebase made easy to write wrong.

The `scope-authority` change fixed *who may define scopes* (one authority). This change fixes *how scopes and authority are enforced*: it defines the enforcement model — typed authority/resource vocabulary, a declarative surface registry with one enforcement point, a `RequireAuthority` combinator with child-ownership resolution, identity derivation by construction, a transport-agnostic enforcement function, a conformance test kit, and a CI coverage guard — so that correct enforcement becomes the **easy** path and each mechanism is closed by a named abstraction (see `design.md` "How each mechanism dies") rather than left to "be careful".

## What Changes

Introduce one new server package `pkg/authz` and a thin route-side declaration layer. No existing guard is deleted; adoption is strangler-style, module-by-module. Concretely:

1. **Typed authority model.** Three grant tiers — `Level` (platform / org / project) — four resource kinds — `Kind` (platform / organization / project / child, where a child is authorized by its owning project's membership) — plus `Principal` (credential + route derived, tagged with its principal kind) and `Resource` (kind + server-resolved id). The invariant: a grant is scoped to a resource family + a specific org/project, exercised only within that scope, with identity never client-promoted.
2. **A declarative surface registry.** Every entrypoint declares `(Kind, Level, Resolve)` once; a single `Enforce(entryID)` middleware applies it, replacing hand-assembled `RequireAuth → RequireProjectTokenScope → RequireProjectMember → RequireScopes(...)` chains.
3. **`RequireAuthority(resource, level)` combinator** with child-ownership resolution built in (kills mechanism 5 by construction).
4. **Identity derivation by construction.** `DerivePrincipal` resolves the caller's org/project from credential + route; `X-Project-ID` is demoted to a *hint* validated against the credential (kills mechanism 3).
5. **One transport-agnostic `Authorize` function** called by HTTP routes, in-process dispatch (`mcp.Service.ExecuteTool`), SSE, and public share alike (kills mechanism 7 — the exact gap the MCP audit found).
6. **A conformance test kit.** Given a registry entry, auto-generate the caller-class matrix (unauthenticated, member, non-member, foreign-project, bare-scope token, account token, superadmin, agent, share) and run each row against the real enforcement path, so every module gets the same 6–9-row test for free instead of ad-hoc tests.
7. **A CI coverage guard.** Fail the build when a registered route lacks an authority declaration or a conformance test, or when a handler is not backed by an entry (closes the "untested / undeclared route" subset of mechanism 8; full closure also requires the no-skip and production-fixture obligations in `design.md`).

The two known live findings are carried as worked examples: the mintable bare `admin` scope, and the unenforced in-process `RequiredScope`/`AgentOnly`.

## Capabilities

### New Capabilities

- `authorization-enforcement`: the authority-tier/principal/resource model and its invariant; the declarative surface registry; the `RequireAuthority` combinator with child-ownership resolution; identity derivation by construction; the transport-agnostic `Authorize` function; the conformance test kit; and the CI coverage guard.

### Modified Capabilities

- *(none — this change is additive. It defines a new enforcement layer that the `scope-authority` scope-resolution model feeds into, but alters no existing requirement.)*
