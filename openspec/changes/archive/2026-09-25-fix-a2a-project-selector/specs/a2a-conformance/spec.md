## ADDED Requirements

### Requirement: A2A error reasons distinguish client errors from server faults
The A2A error-envelope mapping SHALL preserve the distinction between client mistakes and genuine server faults. A 4xx status SHALL map to its own reason — 400 to `INVALID_ARGUMENT`, 401 to `UNAUTHENTICATED`, 403 to `PERMISSION_DENIED`, 404 to `NOT_FOUND`, 405 to `METHOD_NOT_ALLOWED`, and any other 4xx to `INVALID_ARGUMENT` — and SHALL NOT be reported as `INVALID_AGENT_RESPONSE`. `INVALID_AGENT_RESPONSE` SHALL be reserved for 5xx failures. Streaming routes (`POST /message:stream`, `POST /tasks/{id}:subscribe`) SHALL surface handler errors in the A2A envelope rather than the platform's generic error shape, unless the response body has already been committed.

#### Scenario: 400 is not reported as a server fault
- **WHEN** a request fails with HTTP 400
- **THEN** the A2A envelope reason is `INVALID_ARGUMENT` and the code maps to HTTP 400

#### Scenario: 5xx stays INVALID_AGENT_RESPONSE
- **WHEN** a request fails with a 5xx status
- **THEN** the A2A envelope reason is `INVALID_AGENT_RESPONSE` and the code maps to HTTP 500

#### Scenario: Streaming route emits the A2A envelope
- **WHEN** an authenticated client calls `POST /message:stream` with no project selector
- **THEN** the response is HTTP 400 with an A2A envelope whose `details[0].reason` is `PROJECT_REQUIRED`, not the platform's generic 500 error shape
