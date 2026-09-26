## Purpose

Extend the gateway objects browser (`GET /objects`) with a third unified search mode and a knowledge-search answer box ("Ask the graph"), while pinning the tenant-isolation contract both new calls rely on: the project is derived server-side from the signed session, and the backing `/api/search/unified` and `/api/projects/{id}/query` endpoints enforce project membership.

## ADDED Requirements

### Requirement: Search objects in unified mode

The objects page SHALL offer a "Unified" search mode alongside full-text and hybrid. A unified search SHALL call `POST /api/search/unified` with `resultTypes=graph`, ranking graph objects via lexical + vector + relationship context + fusion, and SHALL render the top 25 graph hits with a relevance score. Type and branch filters SHALL ride along with the query.

#### Scenario: Unified search

- **WHEN** the user enters a query with the unified mode selected
- **THEN** the query is sent to `POST /api/search/unified` with `resultTypes=graph`, and the graph hits are ranked by relevance

#### Scenario: Unified rows omit unavailable badges

- **WHEN** unified results are rendered
- **THEN** each row shows the type, label, and relevance score, and SHALL NOT render status/embedding badges that unified results do not carry

#### Scenario: Unified results are capped to the top 25

- **WHEN** a unified search matches more than 25 objects
- **THEN** only the top 25 ranked hits are rendered

### Requirement: Ask a grounded question

The objects page SHALL offer an "Ask the graph" box that accepts a natural-language question and renders a grounded answer. The answer SHALL be rendered as sanitized markdown, and the response SHALL carry a session id when the stream reports one.

#### Scenario: Question answered

- **WHEN** the user submits a non-empty question
- **THEN** the gateway calls `POST /api/projects/{id}/query` and renders the concatenated grounded answer in place

#### Scenario: Loading indicator

- **WHEN** a question is in flight
- **THEN** a loading indicator is shown while the answer resolves

#### Scenario: Backend failure

- **WHEN** the query endpoint fails
- **THEN** an inline error state is shown in place of the answer, without crashing the page

#### Scenario: Empty question

- **WHEN** the submitted question is empty or whitespace-only
- **THEN** no query is issued and the browser redirects back to the objects page

#### Scenario: Interrupted stream

- **WHEN** the SSE stream ends without a terminal `done`/`[DONE]` event
- **THEN** the gateway surfaces an error rather than rendering a partial answer as grounded

### Requirement: Membership enforcement behind search and knowledge query

The objects browser SHALL derive the project server-side from the signed session — picking only from the user's own projects with no client-supplied override — and SHALL forward it to backing endpoints that enforce project membership. This posture MUST be preserved: a future change SHALL NOT silently drop the membership enforcement behind the unified-search (`POST /api/search/unified`) or knowledge-query (`POST /api/projects/{id}/query`) endpoints.

#### Scenario: Project derived from the session

- **WHEN** a signed-in user opens the objects browser
- **THEN** the active project is resolved from the user's own project list (`resolveDefaultProject`), never from a client-supplied value

#### Scenario: Non-member with a foreign project

- **WHEN** a caller addresses a project whose owning organization they do not belong to
- **THEN** the backing endpoint returns 403 and no object data or answer is disclosed

#### Scenario: Token bound to a different project

- **WHEN** a project-bound API token addresses a different project
- **THEN** the backing endpoint returns 403 on the token-binding mismatch

#### Scenario: Unknown project

- **WHEN** the addressed project does not exist
- **THEN** the backing endpoint returns 404

#### Scenario: Unauthenticated caller

- **WHEN** the backing endpoint is called without authentication
- **THEN** it returns 401
