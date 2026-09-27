# Design — surface→authority matrix: method & maintenance

## Context

This change consolidates four read-only audit reports into one durable matrix (`matrix.md`). It is documentation: no production code, no spec delta (`skip_specs: true`). The matrix is the *current* answer to "who may touch which surface", and it is meant to be kept honest — not to freeze the current (partially broken) state.

See `proposal.md` for the "why"; `matrix.md` for the artifact itself.

## Goals / Non-Goals

**Goals:** (1) one checked-in, scannable surface→authority inventory with verdicts; (2) an honest normalization of findings that were stale/fixed/in-flight when the reports were written; (3) an explicit link to the `authorization-enforcement` abstraction (#991) so the matrix is maintained by construction, not by re-auditing.

**Non-Goals:** fixing any finding here; changing any guard; altering `scope-authority`; re-deriving the eight mechanisms (those live in #991).

## Decisions

### How the audit was performed

Four read-only lanes, each in an isolated worktree, each given a domain partition and the `scope-authority` spec plus the "already-fixed" list:

1. **auth core** (`pkg/auth/**`, `domain/apitoken/**`, `domain/superadmin/**`) — guard-primitive semantics, scope-vocabulary expansion, mint-authority paths, `X-Project-ID` consumption.
2. **MCP + agent surfaces** (`domain/mcp/**`, `mcprelay`, `mcpregistry`, `skills`, `agents/**`, `agentcompat/**`) — transport enforcement matrix, declared-vs-enforced tool scopes, trust propagation (`ExternalFacing`/`TrustedInternal`), public share.
3. **domains A–L** — route registration + guard chain for 20 domains.
4. **domains M–Z** — same for 20 more domains.

Enumeration was by tracing `RegisterRoutes` / `e.Group(...)` registrations and reading the guard chain **in order**, including parent-group middleware, plus handler/service-level ownership checks (the reports distinguished route-level vs handler-level vs service-level enforcement). Tool scopes were enumerated by grepping `RequiredScope`/`SuperadminOnly`/`AgentOnly` across `*_tools.go` and the central `toolRequiredScope` map.

**Re-run:** repeat the same grep/read pass at a new SHA, then diff against `matrix.md`. The concrete commands are preserved in the reports (`/tmp/opencode/audit-*.md`): `grep -rn "\.GET(\|\.POST(\|\.PUT(\|\.PATCH(\|\.DELETE(\|\.Group(" <domain>` per domain, `grep -rn "RequireAuth\|RequireProjectTokenScope\|RequireProjectMember\|RequireSuperadminFull\|RequireAPITokenScopes\|RequireScopes" <domain>`, and `grep -rn "RequiredScope\|SuperadminOnly\|AgentOnly\|TrustedInternal" domain/mcp domain/agents`.

### Normalization rule

Every finding was re-checked at the audited SHA (`10f2c6f4a`) before being recorded. A finding's status is one of `fixed #N` (merged), `in-flight #N` (open PR), `open` (verified present), or `stale`/`refuted` (audit source wrong or code changed). The three stale/refuted rows and the seven fixed rows are listed explicitly in `matrix.md` §3 so the normalization is auditable.

### The maintenance story — tie to `authorization-enforcement`

The matrix rots exactly when a guard changes and nobody updates the doc. The `authz-abstraction` change (#991) makes this mechanically checkable:

- Its **declarative surface registry** (`B2`) requires every entrypoint to declare `(Kind, Level, Resolve)`; its **CI coverage guard** (`B7`, `TestRegistryComplete`) fails the build when a route is registered but not declared, or declared but without a conformance matrix.
- Once that lands, the matrix is the *human-readable projection* of the registry: **each registry entry is one matrix row; the `(Kind, Level)` declaration is the "resource tier" + "correct authority" columns; the conformance matrix is the "verdict" column.**

So the invariant becomes: **a route must declare an authority (registry), and the matrix must match the registry (this doc).** Drift between the two is a review-time failure — a PR that adds a route but does not add a registry entry *and* a matching matrix row is rejected. Until `pkg/authz` exists, the matrix is maintained by the re-run procedure above; after it lands, the matrix is regenerated/checked from the registry.

The eight mechanisms are the *why* of each row's verdict; the abstraction's `C` table (`How each mechanism dies`) is the *how* it gets fixed. The matrix records the mechanism per row so a fix can be traced to the abstraction component that closes it.

## Open-finding → abstraction mapping

Each open `MISMATCH`/`UNKNOWN` row in `matrix.md` §2 maps to the `authz-abstraction` component that would prevent it:

| Open finding | Mechanism | Preventing abstraction |
|---|---|---|
| chat conversation by-id ignores owner | 5 | `RequireAuthority` (`B3`) with `KindChild` + conversation→owner resolver |
| health `/api/metrics/jobs` client-supplied project | 3 | `DerivePrincipal` (`B4`) — project derived, query param demoted to hint |
| invites revoke = membership not admin | 2 | `Level >= Kind.RequiredLevel()` assertion (`B1`/`B2`) — org write needs `LevelOrg` |
| blueprints global catalogue writable by any project member | 2 | `KindPlatform`/`KindOrganization` declaration for the global namespace |
| projects CRUD missing membership | 4/3 | `Enforce(entryID)` folds membership into project entries by construction |
| users/search global email enumeration | 1/4 | `RequireAuthority` over a resource (not a bare `org:read` scope) + org-scoped entry |
| schemas global pack trusts `X-Project-ID` | 3 | `DerivePrincipal` — `X-Project-ID` demoted to validated hint |
| sessiontodos child ownership | 5 | `KindChild` + session→owner resolver |
| search retrieval-trace not project-scoped | 5 | `KindChild` (trace→project) resolver |
| orgs membership-only admin ops (`member` role) | 2 | `Kind.RequiredLevel()` — org admin ops = `LevelOrg` = `org_admin`, not mere membership |
| tracing `/api/traces` scope-only | 1/5 | `RequireAuthority` over a platform/trace resource, not a bare `admin:read` scope |
| agent CRUD/trigger MCP tools no `RequiredScope` | 1/7 | `AuthorizeTool` projection (`E2`) — one scope→`(Kind,Level)` map, enforced on every transport |
| skills `GetSkill` unscoped read | 5 | `KindChild` (skill→org/project) resolver |
| in-process `ExecuteTool` skips `RequiredScope`/`AgentOnly` | 7 | `Authorize` (`B5`) called from `mcp.Service.ExecuteTool` — the exact `E2` worked example |

## Risks / Trade-offs

- **The matrix describes a moving target.** [Risk] A guard changes between the audited SHA and merge → row goes stale. → Mitigation: every row carries its audited SHA (§1), and the maintenance story ties future accuracy to the registry guard rather than to re-auditing.
- **Verdict confidence is uneven.** [Risk] The domains reports were read at `2fc3b66a6`, well behind the audited SHA. → Mitigation: the intervening code commits (#981/#982/#985/#995/#997/#998/#1002) were re-verified directly at the audited SHA — backups (`#997`), the webhook/`trigger_agent`/`call_agent` trust sites (`#998`), and `/api/diagnostics`+`/debug` (`#995`) moved open→fixed, invites (`#985`) merged (its DELETE row remains open), and search fusion (`#1002`) does not touch the retrieval-trace authz row.
- **"Unknown" intent (blueprints global, bare `admin` scope) is recorded, not resolved.** [Risk] Over- or under-claiming severity. → Mitigation: marked `UNKNOWN`/`stale` with the ambiguity stated, rather than asserted.

## Migration Plan

None — documentation-only. No deploy, no rollback.

## Open Questions

None that change this change. Outstanding *code* questions (e.g. `admin` scope disposition, child-resolver enumeration) are owned by `authz-abstraction` #991, not here.
