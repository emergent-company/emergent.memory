## ADDED Requirements

### Requirement: Operator MCP endpoint is dedicated and gated on superadmin_full

Memory SHALL serve platform-operation MCP tools on a dedicated endpoint `GET|POST|DELETE /api/mcp/admin`, separate from the project MCP endpoint `/api/mcp`. The endpoint SHALL be gated by authentication followed by the shared `RequireSuperadminFull` middleware — an active `superadmin_full` grant resolved from `core.superadmins`, not a token scope. An `admin`/`admin:all` token, an `org_admin`, and a `superadmin_readonly` grant SHALL NOT satisfy it. The endpoint SHALL speak the same MCP protocol version(s) and transport as the project endpoint, SHALL resolve no project context, and SHALL NOT require project membership.

#### Scenario: Unauthenticated caller denied

- **WHEN** an unauthenticated caller sends any request to `/api/mcp/admin`
- **THEN** the server responds 401

#### Scenario: Non-superadmin denied at the route

- **WHEN** an authenticated caller without an active `superadmin_full` grant sends any request to `/api/mcp/admin`
- **THEN** the server responds 403 before any MCP session or tool is processed

#### Scenario: admin:all token denied

- **WHEN** a caller holding an `admin:all` token minted by an `org_admin` requests `/api/mcp/admin`
- **THEN** the server responds 403

#### Scenario: superadmin_full admitted without a project

- **WHEN** an active `superadmin_full` principal with no project context initializes a session on `/api/mcp/admin`
- **THEN** the session initializes successfully with no project id
- **THEN** the operator tool catalog is listed

### Requirement: The operator endpoint exposes a defined operator tool catalog

The `/api/mcp/admin` catalog SHALL expose only deployment-wide operator tools, each marked `SuperadminOnly`. The catalog SHALL include:

- Reads: `superadmin-get`, `user-list`, `user-get`, `org-list`, `org-get`, `project-list`, `project-record-get`, `project-member-list`, `email-job-list`, `email-job-get`, `embedding-job-list`, `embedding-job-stats`, `extraction-job-list`, `extraction-job-stats`, `document-parsing-job-list`, `document-parsing-job-stats`.
- Mutations: `user-delete`, `org-delete`, `project-delete`, `superadmin-grant`, `project-member-create`, `project-member-delete`, `service-token-create`, `embedding-job-delete`, `embedding-job-cleanup`, `embedding-job-reset`, `extraction-job-delete`, `extraction-job-cancel`, `document-parsing-job-delete`, `document-parsing-job-retry`.
- The pre-existing operator tools: reads `embedding-status`, `provider-list-org`, `provider-test`, `provider-usage-get`, `trace-list`, `trace-get`; mutators (also `confirm`-guarded) `embedding-pause`, `embedding-resume`, `embedding-config-update`, `provider-configure-org`.

Tool names SHALL follow the `area[-noun]-action` kebab-case convention (lowercase letters, digits, hyphens only, no underscores). Each mutating tool's input schema SHALL require the identifiers it acts on; an unknown or malformed identifier SHALL be rejected with an error naming the offending field, not silently ignored. Operator tools that target an organization SHALL require an explicit `org_id`; they SHALL NOT rely on an ambient auth-context organization on this endpoint.

Pre-existing operator tools re-homed onto this endpoint SHALL be adapted to operate without project or ambient organization context: they SHALL accept explicit target identifiers and SHALL call the underlying service directly, rather than relying on `auth.ProjectIDFromContext` or returning an endpoint redirection. Every mutating tool in the catalog — including the pre-existing `embedding-pause`, `embedding-resume`, `embedding-config-update`, and `provider-configure-org` — SHALL be subject to the confirmation requirement below.

#### Scenario: Operator catalog is listed

- **WHEN** a `superadmin_full` caller requests `tools/list` on `/api/mcp/admin`
- **THEN** the response contains the operator tools and no project-scoped tools (e.g. `entity-query`, `schema-list`)

#### Scenario: Covered operation is callable

- **WHEN** a `superadmin_full` caller calls a read tool such as `user-list` with valid arguments
- **THEN** the call SHALL return the corresponding repository result

#### Scenario: Missing required identifier is rejected

- **WHEN** a caller calls an operator tool whose input requires an identifier without supplying it
- **THEN** the call SHALL fail with an error naming the missing field
- **THEN** no repository operation SHALL be performed

### Requirement: Operator tools are not reachable from the project MCP endpoint

`SuperadminOnly` tools SHALL be excluded from the project MCP endpoint `/api/mcp` catalog and SHALL NOT be callable there, regardless of the caller's superadmin status. Operator authority on the admin endpoint SHALL NOT be reachable through project-token binding or project membership.

#### Scenario: Project endpoint hides operator tools

- **WHEN** any authenticated caller — including a `superadmin_full` principal — requests `tools/list` on `/api/mcp`
- **THEN** the response SHALL NOT contain any `SuperadminOnly` tool

#### Scenario: Project endpoint refuses operator tool call

- **WHEN** any caller calls a `SuperadminOnly` tool name on `/api/mcp`
- **THEN** the call SHALL fail as an unknown method, and no operator operation SHALL be performed

### Requirement: Mutating operator tools require explicit confirmation

Every mutating operator tool — the new mutation tools and the pre-existing mutators `provider-configure-org`, `embedding-pause`, `embedding-resume`, `embedding-config-update` — SHALL require an explicit `confirm` argument. When `confirm` is absent or false, the tool SHALL perform no side effect and SHALL return a structured dry-run that names the intended action and its target identifiers. When `confirm` is true, the tool SHALL perform the operation and report its outcome. A mutating call SHALL NOT execute any repository mutation before confirming.

#### Scenario: Unconfirmed mutation is a dry run

- **WHEN** a `superadmin_full` caller calls a mutating tool without `confirm=true`
- **THEN** the response SHALL describe the intended action and target(s)
- **THEN** no repository mutation SHALL occur

#### Scenario: Confirmed mutation executes once

- **WHEN** a `superadmin_full` caller calls the same mutating tool with `confirm=true`
- **THEN** the operation SHALL be performed exactly once and its outcome returned

#### Scenario: Confirmation does not bypass authority

- **WHEN** a caller without `superadmin_full` calls a mutating tool with `confirm=true`
- **THEN** the call SHALL be denied (403 at the route / fail-closed in-process) and no mutation SHALL occur

### Requirement: Every operator tool call is audited

Each operator tool call on `/api/mcp/admin` SHALL produce an audit record identifying the actor, the tool, the outcome, the duration, and the calling client, in both a queryable store and structured logs. Arguments SHALL be recorded with secrets redacted (provider API keys, service-account JSON, tokens, and any field whose value is a credential). Admin session initialization SHALL also be recorded. Audit SHALL be best-effort: a failure to write an audit record SHALL be logged but SHALL NOT fail the tool call.

#### Scenario: Successful call is recorded

- **WHEN** a `superadmin_full` caller successfully calls an operator tool
- **THEN** an audit record SHALL be written with actor, tool, success outcome, and duration
- **THEN** a structured log line SHALL be emitted for the same call

#### Scenario: Failed call is recorded

- **WHEN** an operator tool call fails
- **THEN** an audit record SHALL be written with the failure outcome and error information

#### Scenario: Secrets are redacted

- **WHEN** an operator tool call carries a credential-bearing argument such as a provider API key or service-account JSON
- **THEN** the audit record and log line SHALL NOT contain the raw credential value

#### Scenario: Audit failure does not fail the call

- **WHEN** the audit store is unavailable
- **THEN** the tool call SHALL still complete and return its result
- **THEN** the audit failure SHALL be logged

### Requirement: In-process agent runs cannot reach operator tools on trust alone

An agent run that dispatches tools in-process SHALL NOT exercise operator authority on the strength of the trusted-internal marker alone. `SuperadminOnly` tools SHALL require a resolved `superadmin_full` platform grant on the in-process path, fail-closed when the grant cannot be resolved. The transport-enforced marker set by the admin endpoint after its own superadmin check remains the only way an operator tool call is not re-gated in-process.

#### Scenario: Trusted non-superadmin run is denied

- **WHEN** a trusted-internal agent run without a `superadmin_full` originating principal attempts to dispatch a `SuperadminOnly` tool in-process
- **THEN** the dispatch SHALL be denied and no operator operation SHALL be performed

#### Scenario: Unresolvable grant fails closed

- **WHEN** the caller's platform grant cannot be resolved during an in-process dispatch of a `SuperadminOnly` tool
- **THEN** the dispatch SHALL be denied
