# Operator MCP Runbook

This is the runbook for the memory server's platform-operator MCP surface. It is
the audited, identity-gated replacement for `admin-tui` and direct SSH/DB
administration for routine platform operations.

## 1. Endpoint and access control

- **Endpoint:** `GET|POST|DELETE /api/mcp/admin` (MCP Streamable HTTP,
  `2025-11-25`, same protocol/transport as the project endpoint `/api/mcp`).
- **Gating:** `RequireAuth()` followed by `RequireSuperadminFull()` — the caller
  must hold an **active `superadmin_full` grant** in `core.superadmins`. The
  authority is resolved from the caller's identity, never from a token scope, so
  the following do **not** satisfy it:
  - `superadmin_readonly` grant (read-only superadmins keep the HTTP
    `/api/superadmin/*` surface),
  - `admin` / `admin:all` tokens (mintable by an `org_admin`),
  - `org_admin` project membership.
- **No project context:** the endpoint resolves no project and requires no
  project membership. Org-targeted tools require an explicit `org_id`; there is
  no ambient auth-context org fallback on this endpoint.

### Connecting an MCP client

Authenticate with a bearer token belonging to a `superadmin_full` principal and
point the client at the base URL path `/api/mcp/admin`. The server advertises
bearer-token auth (no OAuth flow) at `/.well-known/oauth-protected-resource`.

- Initialize once (`initialize`) to obtain an `Mcp-Session-Id`, then send
  `tools/list` / `tools/call` with that session header.
- Unauthenticated requests return `401`; authenticated non-`superadmin_full`
  callers return `403` **before** any session or tool is processed.

## 2. Operator tool catalog

Every operator tool is `SuperadminOnly` (kebab-case). The catalog is served only
on `/api/mcp/admin`; the project endpoint `/api/mcp` never lists or calls these
tools.

### Reads

| Tool | Purpose |
|---|---|
| `superadmin-get` | Current caller's superadmin identity and role |
| `user-list` | Paginated users (search / org filter) |
| `user-get` | One user by `user_id` |
| `org-list` | Paginated organizations with member/project counts |
| `org-get` | One organization by `org_id` |
| `project-list` | Paginated projects (org filter) |
| `project-record-get` | One project record by `project_id` |
| `project-member-list` | Members of a project by `project_id` |
| `email-job-list` | Paginated email jobs (status/recipient/date filters) |
| `email-job-get` | One email job by `job_id` |
| `embedding-job-list` | Graph+chunk embedding jobs (status/type/project/error filters) |
| `embedding-job-stats` | Aggregate embedding job statistics |
| `extraction-job-list` | Object extraction jobs (status/type/project/error filters) |
| `extraction-job-stats` | Aggregate extraction job statistics |
| `document-parsing-job-list` | Document parsing jobs (status/project/error filters) |
| `document-parsing-job-stats` | Aggregate document parsing job statistics |

### Mutations (all `confirm`-guarded)

| Tool | Purpose |
|---|---|
| `user-delete` | Soft-delete a user (`user_id`) |
| `org-delete` | Soft-delete an organization (`org_id`) |
| `project-delete` | Soft-delete a project (`project_id`) |
| `superadmin-grant` | Grant a user `superadmin_readonly` (`user_id`, optional `notes`) |
| `project-member-create` | Add/upsert a project member (`project_id`, `user_id`, `role`) |
| `project-member-delete` | Remove a project member (`project_id`, `user_id`) |
| `service-token-create` | Mint a service account (readonly) + persistent account token (`name`, optional `notes`) |
| `embedding-job-delete` | Bulk-delete embedding jobs (`ids`, optional `job_type`) |
| `embedding-job-cleanup` | Delete orphaned embedding jobs |
| `embedding-job-reset` | Reset dead-letter graph embedding jobs to pending |
| `extraction-job-delete` | Bulk-delete extraction jobs (`ids`) |
| `extraction-job-cancel` | Bulk-cancel queued/processing extraction jobs (`ids`) |
| `document-parsing-job-delete` | Bulk-delete document parsing jobs (`ids`) |
| `document-parsing-job-retry` | Bulk-retry failed/retry_pending parsing jobs (`ids`) |

### Pre-existing operator tools (also on this endpoint)

`embedding-status`, `embedding-pause`, `embedding-resume`,
`embedding-config-update`, `provider-list-org`, `provider-configure-org`,
`provider-test`, `provider-usage-get`, `trace-list`, `trace-get`.

> **Note:** `superadmin-grant` grants **`superadmin_readonly` only** — the
> repository does not expose a full-grant method. `service-token-create` also
> provisions its synthetic service account as `superadmin_readonly`.

## 3. Confirmation protocol for mutations

Every mutating operator tool takes a `confirm` boolean argument — this includes
both the new mutation tools **and** the pre-existing mutators
`provider-configure-org`, `embedding-pause`, `embedding-resume`, and
`embedding-config-update`.

- `confirm` absent or `false` → **dry-run**: no side effect; the result is a
  structured object with `dry_run: true`, `action` (the intended action), and
  `targets` (the target identifiers), plus a human-readable `message`.
- `confirm: true` → the operation runs exactly once and its outcome is returned.

This makes accidental agent-driven deletion require a deliberate second call and
gives the audit trail a visible "previewed" vs "applied" distinction. Confirmation
does **not** bypass authority: the route gate (`superadmin_full`) and the
in-process re-gate both still apply.

Malformed or missing identifiers are rejected **before** the confirmation check
and before any repository operation, with an error naming the offending field.

## 4. Audit trail

Every operator tool call and every admin-session `initialize` is recorded to
`kb.audit_log` **and** to structured logs.

### `kb.audit_log` `event_type` values

- `mcp.superadmin.tool_call` — one operator tool call.
- `mcp.superadmin.initialize` — one admin-session initialize.

### Fields recorded

`event_type`, `outcome` (`success` | `error`), `user_id`, `user_email`,
`resource_type`, `resource_id`, `action` (the tool name, or `initialize`),
`endpoint` (`/api/mcp/admin`), `http_method`, `status_code`, `error_message`
(redacted), `ip_address`, `user_agent`, `request_id` (the `X-Request-ID` when
set, else the session id), and `details` (the tool arguments, redacted, plus
`duration_ms`).

### Secret redaction

Arguments **and** error messages are redacted before either sink. Any value whose
key carries a credential marker — `api_key`, `service_account_json`, `token`,
`password`, `secret`, `credential`, `authorization`, `bearer` — is replaced with
`[REDACTED]`. Map redaction is recursive (nested maps and arrays); free-form
error strings are redacted for `key=value` / `key: value` credential pairs.
Redaction never corrupts the semantics of an ordinary (non-secret) error message.

### Best-effort semantics

An audit write failure logs a warning and **never** fails the tool call —
auditing must not become the availability bottleneck for recovery actions. If
`kb.audit_log` is absent from a given environment's schema, the trail degrades to
structured logs only.

### Retention / access

`kb.audit_log` has **no retention policy** — the trail is append-only and grows
unbounded. Define and enforce a retention/archival policy (and, if desired,
query-only access for `superadmin_readonly`) before relying on the table in
production. The table is queryable by superadmins via the database.

## 5. Release note

Operator tools **moved** off `/api/mcp` onto `/api/mcp/admin`. Any client that
previously called `embedding-*`, `provider-list-org`, `provider-configure-org`,
`provider-test`, `provider-usage-get`, `trace-list`, or `trace-get` on
`/api/mcp` must now authenticate as `superadmin_full` and use `/api/mcp/admin`.
The project endpoint returns `method-not-found` for these tools (they are not
listed and are not callable), regardless of caller superadmin status.
