## MODIFIED Requirements

### Requirement: Unavailable surfaces never expose internal error detail

When a tenancy load fails and the web UI renders the unavailable state, the surfaced message SHALL be a
sanitized, non-technical message. It SHALL NOT expose the raw backend error: no hostname, IP address,
port, URL, transport detail (for example `dial tcp`), or the `memory <status> <code>:` prefix (for example
`memory 503 service_unavailable`).

A backend 5xx response (`502`/`503`/`504` or any other server error), a transport-level failure
(`*url.Error` or `net.Error`), and a client-side timeout (`context.DeadlineExceeded`) SHALL all surface the
same generic "Memory is temporarily unavailable" message. Any other failure SHALL surface a generic
non-technical message, never `err.Error()`.

This covers every tenancy load that renders an unavailable page — including the single-org load
(`GetOrg`) behind `GET /orgs/:id` and its org sub-pages — not only the `ListOrgs` / `ListProjects` /
`ListProjectsIncludingPending` calls redacted by the initial change.

This also covers **every non-tenancy page** whose load failure renders through `pageError`: the agent,
api-token, approvals, backup, blueprint, board, data-source, document, embedding, inbox, MCP
(nodes/registry/servers/shares), object, org-member, run, schedule, schema, schema-pack, session, share,
skill, and usage surfaces. Their displayed error SHALL be routed through the shared redaction helper
(`userFacingError`) so the same sanitized message appears, without changing control flow, branching, or
HTTP status codes. This extends to the two surfaces that render `data.LoadErr.Error()` directly rather than
through `pageError` — the MCP connected-devices relay banner and the API-token reveal branch.

A locally-authored, already-safe message SHALL be preserved rather than remapped to the generic message —
for example the backup page's no-org sentinel copy and its "backup not found" copy. Only genuine backend
failures (a memory HTTP error, a transport failure, or a timeout) are sanitized.

This extends further to the **templated error fields that are rendered directly** (not through
`pageError` and not via `data.LoadErr`): `.PendingErr`, `.ToolsErr`, `.ChatsErr`, `.StatusErr` /
`.CoverageErr`, `.GeneralErr`, `.SaveErr`, `.MCPActionErr`, and the providers `d.Err`. Each such field
SHALL carry an already-sanitized value **at the point it is assigned** on the page/view model, so the
template cannot render raw backend detail. Where a field mixes locally-authored copy with backend
failures (the object-type editor's `SaveErr`, the MCP action banner's `MCPActionErr`), a local
validation/sentinel error SHALL pass through unchanged while a memory HTTP error, transport failure, or
timeout is sanitized. A field already sanitized by its loader and passed through unchanged SHALL NOT be
re-sanitized (no double-wrapping). Control flow, branching, HTTP status codes, and which variant/empty
state renders SHALL be unchanged.

#### Scenario: Transport failure on the landing route

- **WHEN** an authenticated `GET /` resolves and `ListOrgs` returns a transport error whose text carries a
  host and port (for example `dial tcp 172.17.0.1:8080: connect: connection refused`)
- **THEN** the unavailable state renders with the sanitized "Memory is temporarily unavailable" message
- **AND** the rendered page does not contain the host, IP, port, `dial tcp`, or a `memory 5xx` prefix

#### Scenario: Transport failure on the org landing

- **WHEN** an authenticated `GET /orgs/:id` resolves the org but `ListProjectsIncludingPending` returns a
  transport error carrying a host and port
- **THEN** the "Organization unavailable" state renders with the sanitized message
- **AND** the rendered page does not expose the host, IP, port, `dial tcp`, or a `memory 5xx` prefix

#### Scenario: Single-org load failure on the org landing

- **WHEN** an authenticated `GET /orgs/:id` fails to resolve the org because `GetOrg` returns an error (a
  5xx, a transport failure, or a timeout)
- **THEN** the "Organization unavailable" state renders with the sanitized "Memory is temporarily
  unavailable" message
- **AND** the rendered page does not expose the host, IP, port, `dial tcp`, `service_unavailable`, or a
  `memory 5xx` prefix

#### Scenario: Single-org load failure on an org sub-page

- **WHEN** an authenticated org sub-page (`GET /orgs/:id/members`, `/orgs/:id/invite`,
  `/orgs/:id/settings`, `/orgs/:id/settings/general`, or `/orgs/:id/settings/danger-zone`) fails to
  resolve the org because `GetOrg` returns an error
- **THEN** that page's unavailable state renders with the sanitized message
- **AND** the rendered page does not expose the host, IP, port, `dial tcp`, `service_unavailable`, or a
  `memory 5xx` prefix

#### Scenario: Non-tenancy page load failure

- **WHEN** any swept non-tenancy page (for example `GET /agents`, `/documents`, `/skills`, `/sessions`,
  `/board`, `/usage`, or `/objects`) fails its load because the backend returns a 5xx, a transport error,
  or a timeout
- **THEN** that page renders the sanitized "Memory is temporarily unavailable" message
- **AND** the rendered page does not contain the host, IP, port, `dial tcp`, a `memory 5xx` prefix,
  `service_unavailable`, or `Token validation`

#### Scenario: MCP connected-devices relay failure

- **WHEN** the MCP relay registry/sessions load fails on the connected-devices surface (rendered from
  `data.LoadErr` directly, not through `pageError`)
- **THEN** the rendered banner carries the sanitized message
- **AND** does not expose the host, IP, port, `dial tcp`, or a `memory 5xx` prefix

#### Scenario: Backups no-org copy is preserved

- **WHEN** the backups page cannot resolve an org because the active project carries none (the deliberate
  `errBackupsNoOrg` sentinel)
- **THEN** the page renders that specific, safe no-org copy
- **AND** does not render the generic "Something went wrong." message

#### Scenario: Backups backend failure is sanitized

- **WHEN** the backups page fails because the backend returns a 5xx or a transport error
- **THEN** the page renders the sanitized message
- **AND** does not expose the host, IP, port, `dial tcp`, or a `memory 5xx` prefix

#### Scenario: Templated error field failure is sanitized

- **WHEN** an affected templated field (`.PendingErr`, `.ToolsErr`, `.ChatsErr`, `.StatusErr`,
  `.CoverageErr`, `.GeneralErr`, `.MCPActionErr`, or providers `d.Err`) is populated from a backend
  failure — a 5xx/503, a transport error, or a timeout
- **THEN** the rendered page carries the sanitized "Memory is temporarily unavailable" message
- **AND** the rendered page does not contain the host, IP, port, `dial tcp`, `memory 5`, a `memory 5xx`
  prefix, `service_unavailable`, or `Token validation`

#### Scenario: Mixed local copy survives on a sanitized field

- **WHEN** the object-type editor's `SaveErr` carries a local validation error (for example a property
  needs a type) or the MCP action banner's `.MCPActionErr` carries a 4xx/sentinel message (a duplicate
  label, already-exists, or "this agent has no active MCP endpoint")
- **THEN** that specific, safe, locally-authored copy is rendered unchanged
- **AND** only genuine backend failures on the same field are replaced by the sanitized message
