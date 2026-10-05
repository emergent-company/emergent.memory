## Context

See `proposal.md` — Why. Two facts about the current server shape drive this design:

- **The authority and transport already exist.** `domain/mcp` already serves deployment-wide `SuperadminOnly` tools (`embedding-status/pause/resume/config-update`, `provider-list-org/configure-org/test/usage-get`, `trace-list/get`). `ToolDefinition.SuperadminOnly` is gated on `auth.IsSuperadminFull` (a `core.superadmins` row with `role = 'superadmin_full'`), enforced at every HTTP transport and re-checked in `ExecuteTool` (fail-closed). `superadminOnlyToolNames` and the scope vocabulary are derived from `dynamicToolBuilders`, so a new builder is gated automatically.
- **The operations already exist.** `domain/superadmin.Repository` implements every operator query and mutation this change exposes (`ListUsers`, `SoftDeleteUser`, `ListOrganizations`, `SoftDeleteOrg`, `ListProjects`, `SoftDeleteProject`, `List/Add/RemoveProjectMember`, job list/stats/delete/cancel/retry/cleanup/reset, `GrantSuperadminToUser`), and `domain/apitoken.Service` mints service tokens. No new persistence is needed except the audit trail, and `kb.audit_log` already exists.

## Goals / Non-Goals

**Goals**

- One identity-gated, endpoint-scoped, audited operator surface served by the memory server.
- Cover the operations that today force operators to use `admin-tui` or direct DB/SSH access.
- Keep project MCP behaviour and authority exactly as-is.

**Non-Goals**

- A new UI. `admin-tui` retirement is an infra-repo follow-up; this change only makes it possible.
- Host/infra actions (deploy, restart, migrate) — those stay CI/Ansible.
- Read-only superadmin MCP access.

## Decisions

**Split endpoint, not a flag on the project endpoint.**
The project MCP endpoint enforces project-token binding and org membership. Riding operator tools on it means operator authority is reachable only alongside project context, and leaves the two surfaces' authority stories entangled. A dedicated `/api/mcp/admin` group (the same transport handler in admin mode) makes the operator surface's gate, catalog, and audit self-contained and lets the project endpoint shed operator tools entirely. Rejected: keeping operator tools on `/api/mcp` behind the existing conditional filter — it preserves a second, redundant path to deployment-wide authority.

**`superadmin_full` only; no readonly tier.**
The `SuperadminOnly` marker already means full, and `superadmin_readonly` deliberately does not receive platform write authority. Introducing a partial MCP tier would add a second authority axis to a mechanism designed around one. Read-only superadmins keep the HTTP surface. Rejected: a `SuperadminReadOnly` marker — deferred until there is a concrete read-only operator client.

**Reuse the transport, branch at the project seams.**
`StreamableHTTPHandler` is project-aware at exactly four points: `authorizeProjectClaim` (initialize), `GetToolDefinitionsForProject` (tools/list), instance-scope + `requiresProject` (tools/call), and session project. Admin mode skips project claim, serves the operator catalog, and executes with an empty project. This avoids duplicating session/SSE/JSON-RPC plumbing while keeping project logic out of the operator path. It is a distinct fx-provided instance so both endpoints coexist.

**A `SuperadminToolHandler` seam, like the other cross-domain handlers.**
`domain/mcp` must not import `domain/superadmin` into its constructor graph. Following `EmbeddingControlHandler` / `BlueprintToolHandler`, define the interface in `domain/mcp` and provide the adapter in `cmd/server/main.go` under `MCP` + `SuperAdmin`, `optional:"true"` and nil-safe (fail-closed when the feature is off).

**Confirmation is an explicit argument, checked before any side effect.**
Mutation tools take `confirm bool`. Absent or false returns a structured dry-run (target ids, intended action) and performs nothing. This makes accidental agent-driven deletion impossible without a deliberate second call, and gives the audit trail a visible "previewed" vs "applied" outcome. Rejected: a separate confirm round-trip endpoint — more complexity for the same guarantee.

**Audit to both `kb.audit_log` and structured logs.**
`kb.audit_log` already carries the right shape (`event_type`, `outcome`, `user_id`, `resource_type/id`, `action`, `endpoint`, `http_method`, `status_code`, `error_*`, `ip_address`, `user_agent`, `request_id`, `details`). Writing rows there gives a queryable trail; structured slog gives real-time tailing, matching the `sandbox/audit.go` convention. Arguments are redacted (API keys, service-account JSON, tokens) before either sink. Audit is best-effort: a write failure logs a warning and never fails the tool call, so auditing cannot become the availability bottleneck for recovery actions.

## Risks / Trade-offs

- **Hard split relocates operator tools** → Existing superadmin clients that call `embedding-*`/`provider-*`/`trace-*` on `/api/mcp` break. Mitigation: scan in-repo clients (web-ui, CLI, docs) before merge; release-note the move; if an external client depends on it, stage an additive deprecation window first.
- **`provider-configure-org` / `provider-list-org` resolve org from ambient auth context** → On the admin endpoint there is no ambient project/org. Mitigation: the operator tools require an explicit `org_id` (no ambient fallback on the admin path).
- **Some re-homed provider tools assume project/ambient context** → `provider-test` resolves credentials via `CredentialService.Resolve`, which returns nothing when `auth.ProjectIDFromContext` is empty (`domain/provider/service.go`), and `provider-usage-get` currently returns an endpoint-redirect placeholder (`domain/mcp/provider_tools.go`). Mitigation: the re-homed tools take explicit target arguments and call the underlying service directly (task 2.6).
- **Audit table grows unbounded** → `kb.audit_log` has no retention policy. Mitigation: define retention/archival in the operator runbook; the trail is append-only and queryable by superadmins.
- **A broad operator tool catalog is itself a large blast radius** → It is identity-gated, endpoint-scoped, confirm-guarded, and audited, and it replaces strictly more dangerous SSH/DB paths. Rejected alternative: exposing only reads — leaves routine recovery requiring direct access.
- **`kb.audit_log` is currently unused** → If it is not present in a given environment's schema, audit degrades to structured logs only (best-effort), and the runbook notes the check.

## Migration Plan

No schema migration: `kb.audit_log` exists in the baseline migration. Rollout is additive (new endpoint + catalog) except the deliberate removal of operator tools from `/api/mcp`. Backout is reverting the builder registration and route; no data migration to unwind.
