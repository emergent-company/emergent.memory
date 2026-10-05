## Why

Platform administration is currently spread across several ad-hoc interaction paths that each reach around the application's own authority boundary: `admin-tui` calls `/api/superadmin/*` with static service tokens, the infra `memory-dev-mcp` reads Postgres and hosts directly over SSH, and operators run deploy/maintenance shell scripts by hand. Every one of these is a credential-bearing side channel that must be independently secured, audited, and kept in step with server behaviour.

The memory server already serves a small set of deployment-wide `SuperadminOnly` MCP tools (`embedding-*`, `provider-*`, `trace-*`) through the project MCP surface, gated on an active `superadmin_full` grant resolved from `core.superadmins`. This change turns that seed into a first-class, audited operator MCP surface exposed by the memory server itself — so platform operations stop requiring direct SSH/DB access and collapse into one authenticated, reviewable interaction layer.

This is the memory-server half of the infra programme ("all ad-hoc environment actions via CI/Ansible or a superadmin MCP; direct access only as audited break-glass"). It is a prerequisite for removing `admin-tui` and `memory-dev-mcp`'s mutating paths.

## What Changes

- **New dedicated endpoint `GET|POST|DELETE /api/mcp/admin`** (MCP 2025-11-25 streamable HTTP), gated on `RequireAuth()` + `RequireSuperadminFull()`. It reuses the existing MCP transport and tool execution machinery; it is a distinct route and catalog, not a mode of the project endpoint.
- **Complete operator tool catalog**: read tools (users, orgs, projects, project members, email/embedding/extraction/document-parsing jobs and their stats) and mutating tools (job cancel/retry/delete/cleanup/reset, project-member add/remove, service-token mint, and the platform-global soft-deletes and superadmin grant), wrapping the existing `superadmin.Repository` operations.
- **All operator tools require `superadmin_full`** (the existing `SuperadminOnly` marker). No `superadmin_readonly` MCP tier is introduced; read-only superadmins continue to use the HTTP surface.
- **Hard split**: `SuperadminOnly` tools are removed from the project `/api/mcp` catalog and are reachable only on `/api/mcp/admin`, so operator authority never rides the project token/membership path.
- **Mutating tools require explicit confirmation**: a mutating call without `confirm=true` returns a dry-run description and performs no side effect.
- **Audit trail**: every operator tool call (and admin session initialize) is recorded — actor, tool, redacted arguments, outcome, duration, and client — to the existing `kb.audit_log` table and to structured logs. Audit failure never fails the tool call.
- **No breaking change to project MCP usage**: non-operator tools and their authorization are unchanged. The only behaviour change is that operator tools move endpoints (called out as a release note).

## Capabilities

### New Capabilities

- `superadmin-mcp`: the memory server's operator MCP surface — its dedicated endpoint and gating, the operator tool catalog and coverage, the project-endpoint exclusion, explicit confirmation for mutations, and the audit trail.

### Modified Capabilities

- None. The change adds a new operator surface and relocates existing operator tools onto it; project-scoped MCP tools and their authority are unchanged.

## Impact

- **`apps/server/domain/mcp/`**: new operator tool definitions and handlers (`superadmin_tools.go`), a `SuperadminToolHandler` seam wired to `domain/superadmin.Repository` and `domain/apitoken.Service`, a dedicated admin transport instance and route (`routes.go`, `streamable_http_handler.go`, `module.go`), dispatch cases (`service.go`), and audit (`superadmin_audit.go`).
- **`apps/server/cmd/server/main.go`**: adapter wiring under the `MCP` + `SuperAdmin` feature flags.
- **`apps/server/domain/superadmin/`**: reused unchanged; its repository methods become the operator tool implementations (no HTTP dependency).
- **Persistence**: no migration. Audit rows use the existing `kb.audit_log` table (`migrations/00001_baseline.sql`), which has the required columns and no Go writer today.
- **External consumers**: `admin-tui` and infra `memory-dev-mcp` can be retired once this ships (infra-side follow-up). The change is additive except for the deliberate relocation of the existing operator tools off `/api/mcp`: any in-repo or external client that calls `embedding-*`, `provider-*`, or `trace-*` on `/api/mcp` must move to `/api/mcp/admin`, so rollout coordination is required, not optional (see design — Risks).
- **Security posture**: operator actions become identity-gated (`superadmin_full`, not token scope), endpoint-scoped, and audited; direct SSH/DB administration is no longer required for routine operations.

## Non-Goals

- Removing `admin-tui` or `memory-dev-mcp` (infra-repo follow-up).
- A `superadmin_readonly` MCP tier.
- Host-level actions (deploys, container restarts, migrations) — those remain CI/Ansible, not MCP.
- Changing existing project-scoped MCP tools or their authorization.
