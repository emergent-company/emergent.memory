# MCP authorization class — decision recommendation

> Issue: `emergent-company/emergent.memory#1041` — *fix(mcp): MCP tools call the service layer directly, bypassing HTTP-handler-level authorization (systemic class)*
> Status: **decision recommendation** (analysis deliverable — no code change here).
> Audited SHA: `c84cfc33` (this worktree `docs/mcp-authz-class-decision`).

## TL;DR

Authorization is enforced **per entrypoint**, so the same data is reachable with different rules. There are **two distinct shapes**, and they need **different answers** — a single mechanism will not close both:

- **Shape A — service-call bypass**: an MCP tool calls a repository/service directly, so the handler-level gate never runs. *Fix = move the check into the service/store boundary as a shared helper both entrypoints call.* (The skills fix in #1040 is the proven pattern.)
- **Shape B — transport guard gap**: the MCP route/group (or the in-process dispatch path) lacks a guard its HTTP equivalent has, or an existing helper mis-classifies a caller class. *Fix = one authority check at the choke point (`ExecuteTool`), plus a typed authority vocabulary that stops bare `admin` from gating project data.*

**Recommendation: hybrid (Option C)** — (1) apply the #1040 shared-helper pattern to the two remaining Shape A bypasses now; (2) land `pkg/authz` + `AuthorizeTool` (the `authz-abstraction` E2 worked example) for the Shape B in-process gap; (3) add an external-package parity test as the regression guard. Nothing here depends on #812 §7/§8.

---

## 1. Parity audit (empirical, at `c84cfc33`)

### 1.1 Scope

| Surface | Count |
|---|---|
| MCP tools (static definitions) | **139** = 131 in `GetToolDefinitions()` + 2 `session-todo-*` + 5 agent-endpoint (`call_agent`…`list_sessions`) + 1 hidden built-in `set_session_title` |
| MCP route registrations | **25** across **7** auth groups |
| MCP transports | **3** HTTP (legacy JSON-RPC, SSE, streamable HTTP) + 1 per-agent endpoint + 1 in-process dispatch (`ExecuteTool`) |

Tool→HTTP-equivalent authority was mapped for every tool; verdicts below are the ones that are *not* plain parity.

### 1.2 MCP routes/groups vs HTTP authority

| MCP route/group | Guard (file:line) | HTTP-equivalent authority | Verdict |
|---|---|---|---|
| `GET /.well-known/oauth-protected-resource` | none (`routes.go:13`) | discovery doc (public) | parity |
| `GET /api/mcp/install` | none (`routes.go:18`) | redirect | n/a |
| `GET /api/mcp/bundle` | `?token=` hash validated (`bundle.go:181-198`) | bearer token | parity |
| `g` = `/api/mcp` (unified/rpc/sse) | `RequireAuth`+`RequireProjectTokenScope`+`RequireProjectMember` (`routes.go:22,31,32`) + per-tool check | project membership | parity |
| `pg` = `/api/projects/:projectId/mcp/share|bundle` | `RequireAuth` → role check `project_admin` (`share.go:64-70`, `bundle.go:83-89`) | project admin | parity |
| `adminGroup` = `…/mcp/shares*`, `/tools` | `RequireAPITokenScopes("admin")` (`routes.go:65`) + `EnsureProjectAdmin` | project admin | parity |
| `ag` / `endpointGroup` / `keyGroup` (agent MCP endpoint + keys) | `RequireAuth`+`RequireAPITokenScopes("admin")` (`routes.go:83,88,95`) | project admin | parity |
| `aeg` = `/api/mcp/agents/:agentId` | `RequireAuth` → `hasAgentCallScope` → `AuthorizeAgentEndpoint` (`agent_endpoint_handler.go:54`, `agent_mcp_endpoint.go:222`) | agent-share key only | parity |

### 1.3 MCP tool surface vs HTTP authority — the non-parity rows

| Tool(s) | MCP declaration (file:line) | HTTP equivalent (file:line) | Verdict |
|---|---|---|---|
| `blueprint-create` | `schema:write` (`mcp/blueprints_tools.go:30`); handler drops `ProjectID` when empty → **global** create (`blueprints/mcp_tools.go:91-94`) | global create gated by `requireSuperadminFull` (`blueprints/handler.go:31,79-83`) | **missing** (Shape A) |
| `blueprint-publish` | `schema:write` (`mcp/blueprints_tools.go:94`); handler `PublishBlueprint` w/o superadmin gate (`blueprints/mcp_tools.go:130-141`) | publish gated by `requireSuperadminFull` for global target (`blueprints/handler.go:212-216`) | **missing** (Shape A) |
| `session-get-messages` | `graph:read` (`mcp/service.go:789,1606`); `GetConversationFullHistoryRaw` takes only `acpSessionID`, no project/owner predicate (`service.go:3742-3766`, `agents/repository.go:3351-3352`) | REST history checks ownership then `GetConversationFullHistory` (`agents/handler.go:348-370`) | **missing** (Shape A) |
| `token-list`/`-create`/`-get`/`-revoke` | `admin` (`token_tools.go:21,32,52,68`) | project membership trio (`apitoken/routes.go:20-22`) | **weaker** (Shape B, mech 2) |
| `provider-configure-project` | `admin` (`provider_tools.go:72`); no `assertCallerOwnsProject` | `assertCallerOwnsProject` owning-org membership (`provider/access_control.go:53`, `routes.go:41-46`) | **weaker** (Shape B, mech 2) |
| `provider-models-list` | `admin` (`provider_tools.go:104`) | `RequireAuth` catalog (`provider/routes.go:62`) | **weaker** (Shape B, mech 2) |
| `project-create` | `admin` (`service.go:275`, map `:1551-1614`) | `authorizeOrgAdmin` of `req.OrgID` (`projects/service.go:232`) | **weaker** (Shape B, mech 2/3) |
| `skill-get`/`skill-update`/`skill-delete` | `skills:read`/`skills:write`; now call shared `AuthorizeSkillAccess`/`AuthorizeSkillWrite` (`skills_tools.go:159,224,266`; `skills/store.go:256,298`) | same helpers (`skills/store.go:256,298`) | **parity — fixed #1040** |
| `agent-create`/`update_agent`/`agent-delete`/`trigger_agent` (+defs/runs) | `agents:write`/`agents:read` via `agentToolRequiredScope` (`service.go:1624-1649`) | `RequireAPITokenScopes("agents:write")`+membership (`agents/routes.go:30`) | **parity — fixed #1047** |
| `trace-list`/`trace-get` | `SuperadminOnly: true` (`trace_tools.go:22,58`) | instance-wide requires `superadmin_full` (`tracing/handler.go:89`) | parity (note: re-keyed from `admin`→superadmin) |

**Cross-cutting (Shape B, not a single row):** the in-process dispatch `mcp.Service.ExecuteTool` (`service.go:1929-2017`) enforces the share allowlist, `set_session_title`, `SuperadminOnly`, and — for *untrusted* runs only — `AgentOnly` and the literal scope `"admin"` (`:2009-2017`). It does **not** enforce arbitrary `RequiredScope` values (`graph:read`, `skills:write`, `agents:write`, `schema:write`, …). Those are enforced only in the three HTTP transports (`handler.go:322-367`, `sse_handler.go:342-383`, `streamable_http_handler.go:584-625`). An agent run reaching `ExecuteTool` in-process therefore bypasses per-tool scope enforcement for every scoped tool — mechanism 7, carried as the `authz-abstraction` E2 worked example.

### 1.4 Headline numbers

| Verdict | Count |
|---|---|
| `missing` (Shape A, **still open**) | **3** tool declarations: `blueprint-create`, `blueprint-publish`, `session-get-messages` |
| `weaker` (Shape B, wrong authority level — bare `admin` on project data) | **7** tools: `token-*` ×4, `provider-configure-project`, `provider-models-list`, `project-create` |
| `parity — fixed` (were Shape A/B, closed by #1040/#1047) | skills (5 tools), agents (18 tools) |
| cross-cutting transport gap (mechanism 7, in-process) | **all** scoped tools, reached in-process only |
| `parity` / `n/a` (graph, schema, search, documents, branches, journal, web, relay, superadmin-operator, agent-endpoint) | remaining ~100 |

---

## 2. Options

### Option A — service boundary / shared seam (`authz-abstraction`)

**What it takes.** Land `pkg/authz` (typed `Level`/`Kind`, `RequireAuthority` with child-ownership resolution, `DerivePrincipal`, transport-agnostic `Authorize`) and a declarative surface registry. Migrate the in-process `ExecuteTool` path to `AuthorizeTool` (E2), then token-mint (E1), then strangler-migrate domains one by one, each with its conformance matrix.

**What it does NOT solve.**
- It is a *design*, not shipped code — it closes nothing until adopted per-module.
- It does **not** by itself close the two live Shape A bypasses: `AuthorizeTool` enforces a tool's *declared* `RequiredScope`, but `blueprint-create` and `session-get-messages` already declare a (correct-looking) scope; the missing check is *inside* the service (global-write gate, project scoping). Those still need the #1040 shared-helper treatment.
- Harness blindness (mechanism 8) is closed only by the *combination* conformance kit + CI guard + no-skip lint + production fixtures — the registry guard alone does not.
- Long tail: removing the old `Require*` guards per-module is a multi-release program.

**Incremental adoption.** Strangler, module-by-module; old guards run beside the new layer until each module's matrix is green. No intermediate state is less protected than today.

### Option B — per-entrypoint parity + a parity **test harness**

**The import-cycle problem and a mechanism that works anyway.** `mcp` imports `skills`, `agents`, `mcpregistry` (and reaches `blueprints` via an injected interface), so a test *inside* `package mcp` cannot also import those domains. The working mechanisms:

1. **External test package** (`package mcp_test`) — imports `mcp` **and** the domain, so it can compare both sides. This is *already proven in-tree*: `agent_only_parity_test.go` (`package mcp_test`) imports `mcp` + `mcpregistry` and pins the AgentOnly set (`TestAgentOnlyParityHTTPInProcess`).
2. **Per-domain `tool → required authority` table** owned by the domain package (e.g. `skills` exports its authority map), consumed by a third package test that imports both. Inverts the dependency so the domain owns the truth.
3. **Expectation file** (checked-in JSON/YAML: tool → required authority) compared against `mcp.Service.GetToolDefinitions()`; fails on drift.
4. **`go/analysis` pass** over `RegisterRoutes` / tool definitions flagging handlers or tools without a declared authority (the `authz-abstraction` B7 idea).

**What it detects vs misses.**
- *Detects:* a tool whose declared `RequiredScope`/`AgentOnly`/`SuperadminOnly` drifts from the expected authority for its domain (the "hand-maintained parallel list drifted" failure).
- *Misses:* semantic authority errors (a tool *correctly* declaring `schema:write` that should *actually* be `superadmin_full` is invisible to a table comparison — exactly the blueprint case); service-layer re-authorization; and the in-process path (a declaration test exercises `GetToolDefinitions`, not `ExecuteTool`). It is a **regression guard, not a fix**.

### Option C — hybrid (recommended)

1. **Now (surgical):** replicate the #1040 pattern for the two live Shape A bypasses — extract a `AuthorizeBlueprintWrite`/global-write guard and a session-history ownership predicate into the blueprints / agents store, and call them from **both** the REST handler and the MCP tool. Closes `blueprint-create`, `blueprint-publish`, `session-get-messages` immediately.
2. **Next (structural):** land `pkg/authz` core + wire `ExecuteTool` → `AuthorizeTool` (E2), closing the Shape B in-process gap for *all* scoped tools — the one fix that prevents the class from re-appearing on the agent-run path.
3. **Guard (safety net):** adopt the external-`mcp_test`-package parity test as the standing regression harness for the tools that remain on the old path during the strangler migration.

---

## 3. Recommendation

**Option C.** The two shapes warrant **different answers**, so forcing a single mechanism (A *or* B) leaves one class open:

- Shape A is fixed by **moving the check into the service/store boundary** (the #1040 precedent — "one source of truth, so the two entrypoints cannot drift"), not by another entrypoint-level check.
- Shape B is fixed by **one choke point + a typed vocabulary** (`AuthorizeTool` + no bare `admin` on project data), not by auditing each of 139 tools by hand forever.

**Single highest-leverage first step:** land the #1040 shared-helper pattern for **blueprints global write** and **session-messages scoping** (the two open Shape A instances). Rationale: it is the lowest-risk change (mirrors an already-merged, fail-first-verified fix), it *directly closes the two remaining confirmed instances of this exact issue*, and it establishes the reusable template for every future handler-level gate ("ask what the MCP tool does, and share the helper"). `AuthorizeTool` (E2) is the higher-ceiling structural fix but closes **zero** of the currently-confirmed instances, so it is the close second, not the first step.

**Instances closed by the recommendation:**

| Instance | Status |
|---|---|
| blueprints global write (`blueprint-create`/`-publish`) | **closed by step 1** |
| session messages (`session-get-messages`) | **closed by step 1** |
| skills by-UUID read/write | already closed (#1040) — the template |
| relay-over-HTTP | already closed (#1017, inverse direction) |
| account-token claim | already closed (#1047) |
| in-process scope bypass (mechanism 7) | **not closed by step 1** — closed by step 2 (E2) |
| bare `admin` mint / wrong authority on `token-*`/`project-create`/`provider-*` | **not closed by step 1 or 2** — needs E1 (scope→level mint) |

**Interaction with #812 §7/§8:** none of the recommended steps depend on them. The shared helpers and `AuthorizeTool` consume *already-resolved* scopes/entitlements; they behave identically whether token-scope trust is flipped (§7) or the all-grant is deleted (§8). Conversely, a uniform, tested enforcement layer makes #812 §7/§8 *safer* to land (a flip fails loudly in a conformance matrix rather than silently in prod). The recommendation neither blocks nor waits on #812.

---

## 4. Open questions

1. **Do the two shapes really warrant different answers?** Yes — and the recommendation reflects that. Reconfirmation wanted: is "shared helper for Shape A + `AuthorizeTool` for Shape B" acceptable, or does the team prefer to hold Shape A until `pkg/authz` lands (which would leave `blueprint-create` and `session-get-messages` open longer)?
2. **Global-write asymmetry policy.** The #1040 skills fix deliberately made MCP *refuse* global-skill writes (403) while REST still lets a superadmin write them. Should blueprints adopt the same asymmetry (global blueprint writes REST-only), or should the MCP surface gain a superadmin path for global writes? This is a product decision, not a code decision.
3. **Right authority for the seven `admin`-scoped tools.** Are `token-*`, `provider-configure-project`, `provider-models-list`, `project-create` meant to be *project-admin* (membership) or *platform* (superadmin)? The bare `admin` scope is being deprecated; the re-keying depends on `authz-abstraction` open question #5 (bare `admin` disposition), which must be decided before E1.
4. **Is the in-process agent-run path in scope here**, or deferred to the `authz-abstraction` program? It is mechanism 7 and the recommendation treats it as step 2 — confirm that ordering is wanted versus bundling it into a single authz PR.
5. **Parity-test home.** Which mechanism becomes authoritative — external `mcp_test` package (precedent exists), a per-domain authority table, or an expectation file? Recommend the external package (proven, no new machinery) unless the team wants the `go/analysis` pass now.
