# Surface → Authority Matrix

> **Audited SHA:** `10f2c6f4a0a9affa94e97149063a2aacce9acbb2` (`origin/main`, 2026-09-25 — includes #977, #984, #985, #991, #995, #997, #998, #1002).
> **Source reports** (read-only, at four different SHAs): `audit-core.md` (a6c438476), `audit-mcp.md` (663e2a375), `audit-domains-al.md` (2fc3b66a6), `audit-domains-mz.md` (2fc3b66a6).
> **Method & maintenance:** see `design.md`. **Intended model:** `openspec/specs/scope-authority/spec.md`.

Every row was re-checked against the audited SHA before being recorded. Rows whose audit source was stale are marked `stale`/`refuted` or `fixed #N`, **not** copied verbatim.

## Legend

**Resource tier** — `platform` (everything, terminal) · `org` (one organization) · `project` (one project + its children) · `child` (one child resource owned by a project) · `none` (public).

**Correct authority** — the entitlement that *should* gate the surface per the `scope-authority` model: `superadmin_full` · `org_admin` (of the addressed org) · `project membership` (a membership role in the addressed project) · `child owner` · `none`.

**Verdict** — `OK` (guard matches authority) · `MISMATCH` (guard ≠ authority) · `UNKNOWN` (could not determine).

**Mechanism** (1–8, from `authz-abstraction` #991) — 1 scope-only gate on scoped data · 2 wrong authority level · 3 client-supplied identity trusted · 4 missing membership check · 5 child ownership unchecked · 6 dead/lying authz code · 7 transport inconsistency · 8 harness blindness.

**Status** — `open` (verified present at audited SHA) · `fixed #N` (closed by merged PR) · `in-flight #N` (open PR) · `stale`/`refuted` (audit source wrong or code changed).

## Summary

| Count | |
|---|---|
| Surfaces catalogued (HTTP routes + MCP tools + guard primitives) | ~300 routes · ~120 MCP tools · 13 guard primitives |
| `OK` (collapsed) | ~280 routes / 12 domains |
| `MISMATCH` / `UNKNOWN` (expanded, **open**) | **14** |
| `fixed` (merged, by the time of consolidation) | 7 |
| `stale` / `refuted` | 3 |
| Conflicts resolved | 2 |

---

## 1. Collapsed OK surfaces (uncontroversial)

Guard chains verified at the audited SHA. These are correct as-is.

| Domain / group | Guard chain (current) | Tier | Authority | Verdict |
|---|---|---|---|---|
| `authinfo` `/api/auth/issuer` | none | none | none | OK — discovery doc |
| `authinfo` `/api/auth/me` | `RequireAuth` | none | self | OK |
| `autoprovision` | no HTTP route (in-process on new-user provisioning) | — | — | OK |
| `branches` | `RequireAuth` + scope + handler `RequireProjectMember` | project | project membership | OK |
| `chunking` / `chunks` | `RequireProjectTokenScope`+`RequireProjectMember` + project-scoped store | project | project membership | OK |
| `devtools` `/docs`,`/openapi.json` | none | none | none | OK — intentionally public; `/coverage` debug-only |
| `discoveryjobs` | `:projectId` middleware + `requireJobAccess` | project | project membership | OK |
| `docs` `/api/docs/*` | none | none | none | OK — static embedded docs |
| `email` | no HTTP route | — | — | OK |
| `embeddingpolicies` | handler-level `RequireProjectMember` | project | project membership | OK |
| `events` `/api/events/stream` | `AuthorizeProject` (membership) | project | project membership | OK |
| `extraction` admin / embedding-control / project-embedding | `RequireSuperadminFull` / `RequireProjectMember` | platform / project | superadmin_full / membership | OK |
| `graph` (~46 routes) | `RequireProjectTokenScope`+`RequireProjectMember` + `GetProjectUUID` store scoping | project/child | membership | OK |
| `journal` | membership + project context | project | membership | OK |
| `modelconfig` | `RequireProjectTokenScope`+`RequireProjectMember` | project | membership | OK |
| `monitoring` | project membership + project-scoped queries | project | membership | OK |
| `notifications` | `user_id` predicates (self-scoped) | child | self | OK |
| `provider` | service-level `assertCallerOwnsProject` / `assertCallerOwnsOrg` | project/org | membership | OK |
| `sandbox` `/api/v1/agent/sandboxes` | `RequireSuperadminFull` (all read/write/tool groups) | platform | superadmin_full | OK |
| `sandboximages` | `RequireProjectTokenScope`+`RequireProjectMember` | project | membership | OK (post-#974) |
| `schemaregistry` | `RequireProjectTokenScope`+`RequireProjectMember` | project | membership | OK |
| `scheduler` | no HTTP route | — | — | OK |
| `standalone` | bootstrap mode-gated (`Standalone.IsEnabled`) | — | — | OK |
| `tasks` | `RequireProjectMember` + child scoping by project | project/child | membership | OK |
| `useraccess` / `userprofile` / `useractivity` | self-scoped (`user.ID`) | none | self | OK |
| `apitoken` mint `admin:all` | `checkAdminAllGrant` → `CanGrantAdminAll` (superadmin_full only) | platform | superadmin_full | OK (fixed #982) |
| `superadmin` `CreateServiceToken` | `requireSuperadminRole(full)` | platform | superadmin_full | OK |
| MCP HTTP transports (legacy rpc / streamable / SSE) | per-tool `RequiredScope` + `AgentOnly` + `SuperadminOnly` + share-instance allowlist | project | membership/scope | OK (in-process exception → §2) |

---

## 2. Expanded mismatches (open)

### Platform / mint

**`POST /api/tokens` + `PATCH` scope updates — bare `admin` scope mintable by any authenticated user.**
- Guard: `create`/`CreateAccountToken`/`UpdateScopes` gate only `admin:all` (`checkAdminAllGrant`, `apitoken/service.go:260,446,651,714`); `ValidApiTokenScopes` still admits bare `admin` (`entity.go:103`) and the request DTO `oneof` admits it (`entity.go:64`).
- Tier: platform (scope marker). Correct authority: superadmin_full (or at least org_admin). Verdict: **UNKNOWN** — the *minting* gap is real, but the *blast radius* is gone (see §3, refuted C-F1): bare `admin` no longer gates any platform surface. Residual: a dead/vestigial scope (mechanism 6).
- Mechanism: 2/6. Status: **stale** (the escalation the audit described is closed; the dead scope remains).

**`CreateEphemeral` hardcodes `admin` in the scope set** (`apitoken/service.go:596`). Tier: platform marker. Verdict: **UNKNOWN** — `admin` now gates nothing meaningful. Mechanism: 6. Status: **stale**.

### `agents` / MCP tool authorization

**`mcp.Service.ExecuteTool` — in-process dispatch skips `RequiredScope`/`AgentOnly`.** The three HTTP transports enforce them; the ADK ToolPool path (`service.go:1849`) enforces only superadmin + share-instance allowlist. Tier: project/child. Verdict: **MISMATCH** (transport inconsistency; exploitability depends on underlying service re-authorization). Mechanism: 7. Status: **open** — carried as the `authz-abstraction` E2 worked example.

**Agent CRUD + trigger MCP tools declare no `RequiredScope`** (`agents/mcp_tools.go`: `agent-create` :1075, `update_agent` :1126, `agent-delete` :1170, `trigger_agent` :1184). REST counterparts require `agents:write` (`agents/routes.go`). Tier: project. Verdict: **MISMATCH** — a read-only MCP share token (NULL allowlist) can create/delete/trigger agents over MCP. Mechanism: 1/7. Status: **open**.

### `skills`

**`GET /api/skills/:id` — unscoped read** (`skills/handler.go:115-126` → `FindByID`). Any authenticated user reads any org/project-scoped skill by UUID. Tier: child. Correct authority: org/project membership. Verdict: **MISMATCH**. Mechanism: 4/5. Status: **open**.

### `chat`

**Conversation by-id paths ignore `owner_user_id`.** `ListConversations` filters `owner_user_id = ? OR is_private = false` (`repository.go:47-49`), but `GetByID`/`GetByIDWithMessages`/`GetConversationHistory`/`Update`/`Delete`/`AddMessage` filter `id + project_id` only. Tier: child. Correct authority: owner (or project member for non-private). Verdict: **MISMATCH**. Mechanism: 5. Status: **open**.

### `health`

**`GET /api/metrics/jobs`** — `RequireAuth` only (`health/routes.go:54-56`); project taken from `?project_id` query param for OAuth sessions, else account-wide aggregate. Tier: platform/project. Verdict: **MISMATCH** (cross-tenant metric counts). Mechanism: 3/4. Status: **open**.

### `invites`

**`DELETE /api/invites/:id`** — org membership only, not admin (`invites/handler.go:299,316` `IsUserMember`); Swagger claims "requires project admin access". Tier: org. Correct authority: org_admin. Verdict: **MISMATCH (low)**. Mechanism: 2/5. Status: **open** (distinct from #985).

### `blueprints`

**Global blueprint catalogue writable by any project member, not gated to platform/org authority.** Routes carry `RequireProjectTokenScope`+`RequireProjectMember` but omit `RequireProjectID` on `Create`/`Update`/`Delete` (`blueprints/routes.go:17-35`); `scopeWhere("")` returns `project_id IS NULL` (shared global namespace). Tier: platform (shared namespace). Verdict: **UNKNOWN** (intent unspecified — platform-curated vs user-shared). Mechanism: 2. Status: **open**.

### `projects`

Routes carry only `RequireAuth` + `RequireAPITokenScopes("projects:read")` (no-op for OAuth; `routes.go:13-22`). `Get`/`Update`/`Delete`/`Restore`/`ListMembers`/`RemoveMember`/`Create` have no membership check (`service.go`).

| Surface | Guard | Tier | Authority | Verdict | Mech | Status |
|---|---|---|---|---|---|---|
| `PATCH/DELETE /api/projects/:id` (+ `POST /:id/restore`) | `RequireAuth` + scope | project | membership | MISMATCH | 4/2 | open |
| `GET /api/projects/:id`, `GET /:id/members` | `RequireAuth` + scope | project | membership | MISMATCH | 4 | open |
| `DELETE /api/projects/:id/members/:userId` | `RequireAuth` + scope | child | project_admin | MISMATCH | 4/5 | open |
| `POST /api/projects` (body `orgId`) | `RequireAuth` + scope | org | org_admin | MISMATCH | 3/4 | open |

### `users`

**`GET /api/users/search`** — `RequireAuth` + `RequireAPITokenScopes("org:read")` (`users/routes.go:11-13`); the repo searches `core.user_emails` globally by ILIKE substring. Tier: platform (cross-tenant PII). Verdict: **MISMATCH**. Mechanism: 1/4. Status: **open**.

### `schemas`

**Global pack CRUD trusts `X-Project-ID`.** `g := e.Group("/api/schemas")` with `RequireAuth` only (`schemas/routes.go:12-19`); `GetPack`/`UpdatePack`/`DeletePack`/`CreatePack` use `user.ProjectID` (client header) with no membership check. Tier: project/child. Verdict: **MISMATCH** (cross-tenant write). Mechanism: 3. Status: **open**.

### `sessiontodos`

**`/api/v1/agent/sessions/:sessionId/todos`** — `RequireAuth` only (`sessiontodos/routes.go:11-12`); no ownership predicate in handler or service. Tier: child. Verdict: **MISMATCH**. Mechanism: 5/4. Status: **open**.

### `search`

**`GET /api/search/trace/:traceId`** — `RequireAPITokenScopes("search:read")` (membership on group, but `GetTrace` passes no project filter). Tier: child. Verdict: **MISMATCH** (cross-tenant retrieval-trace read). Mechanism: 5. Status: **open**.

### `orgs`

**Org-scoped admin ops require only membership, and org membership can be `member`.** `requireOrgMember` (membership, not role) gates `Update`/`Delete`/`ListMembers`/tool-settings; `invites/service.go:416-425` writes role `member` for project-scoped invites. A plain `member` can rename or **delete** the org and alter org-wide tool settings. Tier: org. Correct authority: org_admin. Verdict: **MISMATCH**. Mechanism: 2. Status: **open**.

### `tracing`

**`/api/traces/*`** — `RequireScopes("admin:read")` (`tracing/routes.go:16`); scope gate, not tenant authority; `GetTrace` no project filter. Tier: platform. Verdict: **MISMATCH** (known residual — any `admin:read` holder reads all tenants' spans/prompts). Mechanism: 1/5. Status: **open** (pre-identified, not new).

---

## 3. Normalization — fixed / stale / refuted (re-checked, not copied)

| Original finding (source) | Re-check result | Status |
|---|---|---|
| C-F2 `RequireAPITokenScopes("admin")` no-ops for OAuth → platform surfaces unguarded | sandbox/mcpregistry/sandboximages/extraction now use `RequireSuperadminFull` / `RequireProjectTokenScope`+`RequireProjectMember` | **fixed** #947/#958/#964 |
| C-F3 `X-Project-ID` sole scoping on extraction-admin | extraction admin now `RequireProjectTokenScope`+`RequireProjectMember` (job-scoped adds `auth.RequireProjectMembership`) | **fixed** #964 |
| C-admin:all (`CanGrantAdminAll` admits any-org `org_admin`) | `CanGrantAdminAll` narrowed to `superadmin_full` | **fixed** #982 |
| M-transitive depth≥2 (child spawn drops the trust marker) | `executeSingleSpawn` propagates `TrustedInternal: deps.TrustedInternal` (`coordination_tools.go:465`) | **fixed** #981 |
| Webhook receiver `TrustedInternal: true` (public bearer surface reaches internal agents) | `ReceiveWebhook` now `TrustedInternal: false` (`agents/handler.go:1285`); `trigger_agent`/`call_agent` inherit caller trust; worker pool propagates persisted trust — 4 sites, fail-closed | **fixed** #998 |
| Backups unguarded (org / project / restore / database tiers) | org `requireOrgAdmin`, project `requireProjectBackupAuthority`, restore `requireRestoreOwnership` (404-on-foreign), database-backups `RequireSuperadminFull` | **fixed** #997 |
| `/api/diagnostics` + `/debug` unauthenticated | both gated on `RequireAuth` + `RequireSuperadminFull` (`health/routes.go:38-44`); long-running-query report redacted (query → query_length) | **fixed** #995 |
| C-F1 bare `admin` reaches sandbox / MCP registry | those surfaces are membership/superadmin gated; bare `admin` no longer gates them (already stale at the mcp-audit SHA) | **stale** |
| C-F4 `CreateEphemeral` mints `admin` token → platform escalation | `admin` scope no longer gates platform surfaces; residual dead scope only | **stale** |
| M-F2 per-agent MCP endpoint is an external surface reaching internal agents | `call_agent` now inherits caller trust (`agent_run_once.go:148-159`), never forces `true`; untrusted callers fail closed (#998 supersedes #981's force-true) | **refuted** |

---

## 4. Conflicts resolved

1. **Core audit vs MCP audit — "bare `admin` reaches sandbox / MCP registry".** The core audit (a6c438476) listed sandbox/mcpregistry/sandboximages as gated by `RequireAPITokenScopes("admin")`. The MCP audit (663e2a375, later) already showed them membership/superadmin gated. **Judgment: the core audit was stale** — it audited a commit predating #947/#958/#964/#974. The MCP audit is correct.
2. **MCP audit vs #981 — per-agent MCP endpoint trust.** The MCP audit flagged `call_agent` as an external surface that should be untrusted. #981 initially marked it `TrustedInternal: true`. **Judgment: #998 supersedes** — `call_agent` (and `trigger_agent`) now inherit caller trust and reject `internal` targets for untrusted callers, so a project-scoped `mcp:agent-call`/`trigger_agent` caller no longer forces trust; untrusted callers fail closed.
