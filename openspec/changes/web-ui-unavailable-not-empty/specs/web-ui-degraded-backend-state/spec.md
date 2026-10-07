## Purpose

Defines the gateway web UI's degraded-backend-state contract: a backend failure — a 5xx response, a
client-side timeout, or an auth `503 service_unavailable` — SHALL render an explicit "unavailable, your
data is safe" state, and the empty/onboarding ("create your first org/project") state SHALL render only on
a successful authenticated response that reports zero orgs or projects. The two are never conflated.

## ADDED Requirements

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
