## ADDED Requirements

### Requirement: Authenticated A2A endpoints require a project selector
Every authenticated A2A request (extended card, message flow, task read/write, task actions) SHALL resolve a project from the `X-Project-ID` request header or, when the caller authenticated with a project-scoped `emt_*` token, from that token. A project-scoped token SHALL bind the request to its own project: a conflicting `X-Project-ID` header SHALL NOT redirect the request to another project. When neither a token-bound project nor a header is present, the server SHALL respond with HTTP 400, A2A code `-32012` (`INVALID_ARGUMENT`), and reason `PROJECT_REQUIRED`, with a message naming the `X-Project-ID` selector. The unauthenticated global card endpoint SHALL remain unaffected.

#### Scenario: Project-scoped token selects the token project
- **WHEN** a client authenticates with a project-scoped `emt_*` token bound to project A and sends no `X-Project-ID` header
- **THEN** the request is served against project A

#### Scenario: Header cannot redirect a project-scoped token
- **WHEN** a client authenticates with a project-scoped `emt_*` token bound to project A and sends `X-Project-ID: B`
- **THEN** the request is served against project A, not B

#### Scenario: Header selects the project for account-level credentials
- **WHEN** a client authenticates with credentials that are not project-scoped and sends `X-Project-ID: B`
- **THEN** the request is served against project B

#### Scenario: Missing selector returns PROJECT_REQUIRED
- **WHEN** an authenticated client sends `GET /extendedAgentCard` with no token-bound project and no `X-Project-ID` header
- **THEN** the server responds with HTTP 400 and an A2A envelope whose `code` is `-32012`, `status` is `INVALID_ARGUMENT`, and `details[0].reason` is `PROJECT_REQUIRED`

### Requirement: A2A clients send the project selector
The A2A SDK client SHALL send `X-Project-ID` on every request to the authenticated surface — both buffered JSON calls and the `POST /message:stream` SSE path — when a project is configured, and SHALL omit the header when none is configured. The CLI SHALL supply the selected project, with the global `--project` flag overriding the configured project.

#### Scenario: Client sends the selector on the extended card
- **WHEN** an A2A client is constructed with project `P`
- **THEN** `GET /extendedAgentCard` carries `X-Project-ID: P`

#### Scenario: Client sends the selector on the SSE path
- **WHEN** an A2A client is constructed with project `P` and calls `StreamMessage`
- **THEN** `POST /message:stream` carries `X-Project-ID: P`

#### Scenario: No configured project sends no selector
- **WHEN** an A2A client is constructed with no project
- **THEN** requests carry no `X-Project-ID` header

#### Scenario: CLI flag overrides the configured project
- **WHEN** a user runs `memory a2a discover --project P` while a different project is configured
- **THEN** the client selects project `P`
