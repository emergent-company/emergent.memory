# web-ui-degraded-backend-state Specification

## Purpose
Defines the gateway web UI's degraded-backend-state contract: a backend failure — a 5xx response, a
client-side timeout, or an auth `503 service_unavailable` — SHALL render an explicit "unavailable, your
data is safe" state, and the empty/onboarding ("create your first org/project") state SHALL render only on
a successful authenticated response that reports zero orgs or projects. The two are never conflated.

## Requirements

### Requirement: A backend failure renders an explicit unavailable state

When a request that loads org or project tenancy fails — a `ListOrgs`, `ListProjects`, or
`ListProjectsIncludingPending` call returning a 5xx (`502`/`503`/`504`), a client-side timeout, or an auth
`503 service_unavailable` — the web UI SHALL render an explicit unavailable state naming the outage and
reassuring the user their data is safe. It SHALL NOT render the empty/onboarding state, and it SHALL NOT
redirect to the create-org or create-project flow.

The unavailable state SHALL reuse the existing degraded-state mechanism (`pageError`) and carry copy that
distinguishes it from data loss — for example a "Service temporarily unavailable" heading and a "Your data
is safe" message.

#### Scenario: Landing route with a failing org list

- **WHEN** an authenticated `GET /` resolves and `ListOrgs` returns an error (5xx, timeout, or `503
  service_unavailable`)
- **THEN** the gateway renders the app shell with the unavailable state (heading "Service temporarily
  unavailable" and a "Your data is safe" message) with a 200 status
- **AND** does not redirect to `/orgs/new`

#### Scenario: Org landing with a failing project list

- **WHEN** an authenticated `GET /orgs/:id` succeeds in resolving the org but
  `ListProjectsIncludingPending` returns an error
- **THEN** the gateway renders the unavailable state ("Organization unavailable") with a 200 status
- **AND** does not render the "No projects yet / Create project" empty state

#### Scenario: Default-project resolution with a failing project list

- **WHEN** a project-scoped page triggers `resolveDefaultProject` and `ListProjects` returns an error
- **THEN** the gateway renders the unavailable state with a 200 status
- **AND** does not redirect to `/orgs`

### Requirement: Onboarding renders only on a successful authenticated zero-result response

The create-org / create-project onboarding flow SHALL render only when an authenticated tenancy request
succeeds and reports zero results. A zero-result success is the sole trigger; a failed request is never
treated as zero results.

#### Scenario: Successful zero-org response opens onboarding

- **WHEN** an authenticated `GET /` resolves and `ListOrgs` succeeds with an empty result
- **THEN** the gateway redirects to `/orgs/new`

#### Scenario: Successful zero-project response shows the empty state

- **WHEN** an authenticated `GET /orgs/:id` resolves and `ListProjectsIncludingPending` succeeds with an
  empty result
- **THEN** the gateway renders the empty state ("No projects yet / Create project")

#### Scenario: A failure is never reported as zero results

- **WHEN** any tenancy load fails
- **THEN** the gateway never renders the empty/onboarding state for that request

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

### Requirement: Org-scoped list failures on an org sub-page never expose internal error detail

When an org-scoped list load on an org sub-page fails and the page renders its unavailable state —
`ListOrgMembers` on `GET /orgs/:id/members` or `ListOrgToolSettings` on `GET /orgs/:id/settings` — the
surfaced message SHALL be the sanitized, non-technical message from the shared redaction helper. It SHALL
NOT expose the raw backend error (no host, IP, port, URL, `dial tcp`, or `memory <status> <code>:` prefix).

#### Scenario: Org members list failure

- **WHEN** an authenticated `GET /orgs/:id/members` resolves the org but `ListOrgMembers` returns a 5xx or
  transport error
- **THEN** the "Members unavailable" state renders with the sanitized message
- **AND** the rendered page does not expose the host, IP, port, `dial tcp`, or a `memory 5xx` prefix

#### Scenario: Org tool-settings list failure

- **WHEN** an authenticated `GET /orgs/:id/settings` resolves the org but `ListOrgToolSettings` returns a
  5xx or transport error
- **THEN** the "Settings unavailable" state renders with the sanitized message
- **AND** the rendered page does not expose the host, IP, port, `dial tcp`, or a `memory 5xx` prefix
