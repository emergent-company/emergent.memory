## ADDED Requirements

### Requirement: Objects page guards the provenance filter

The `/objects` page SHALL normalize its provenance filter before dispatching
the list request, so the gateway never forwards a provenance value the server
rejects: an empty `actor_type` SHALL clear the provenance mode to `any`, and an
unrecognised `provenance` value SHALL be coerced to `any`.

#### Scenario: Bare provenance cleared

- **WHEN** a request to `/objects` carries `provenance` without `actor_type`
- **THEN** the page dispatches the list request with the provenance filter
  cleared to `any` and does not forward a bare `provenance`

#### Scenario: Invalid provenance coerced

- **WHEN** a request to `/objects` carries a `provenance` value the server does
  not accept (for example `bogus`) alongside an `actor_type`
- **THEN** the page coerces the value to `any` before dispatch

### Requirement: Objects page exposes the provenance control only with a complete actor pair

The `/objects` filter bar SHALL render the provenance control only when the
request carries a complete actor pair (`actor_type` and `actor_id`). Without a
complete pair the control SHALL be absent, so the page cannot build or dispatch
a bare `provenance` filter.

#### Scenario: No actor selected hides provenance

- **WHEN** a request to `/objects` carries no complete `actor_type` + `actor_id`
  pair
- **THEN** the rendered page omits the provenance control

#### Scenario: Agent-scoped browse keeps provenance

- **WHEN** the objects browser is scoped to one agent (a complete `agent` +
  id pair)
- **THEN** the provenance control is rendered and its selection is forwarded
