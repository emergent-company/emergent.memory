# object-browser Specification

## Purpose
Provides a gateway web UI surface for browsing the knowledge graph's objects (entities) and their relationships.

## Requirements

### Requirement: Navigate to the objects page

The app SHALL provide a navigation entry that opens the objects page.

#### Scenario: Open objects from navigation

- **WHEN** a user selects the objects entry in the app navigation
- **THEN** the objects page opens

### Requirement: List objects

The objects page SHALL list the knowledge graph's objects in browse mode as the 25 most-recent per page (newest first), each showing its name, type, status, and embedding status, with a "Load more" control when a further page exists.

#### Scenario: Objects present

- **WHEN** the objects page loads in browse mode and objects exist
- **THEN** the 25 most-recent objects are listed, each showing name, type, status, and embedding status

#### Scenario: More pages exist

- **WHEN** more than one page of objects exists
- **THEN** a "Load more" control is shown and activating it appends the next page of objects

#### Scenario: No objects

- **WHEN** the objects page loads in browse mode and no objects exist
- **THEN** a clear "no objects yet" empty state is shown

### Requirement: Filter objects by type

The objects page SHALL let the user filter the list by object type.

#### Scenario: Filter by type

- **WHEN** the user selects a type filter
- **THEN** only objects of that type are listed

### Requirement: Browse by branch

The objects page SHALL let the user choose which graph branch to browse.

#### Scenario: Choose a branch

- **WHEN** the user selects a branch
- **THEN** the objects on that branch are listed

#### Scenario: Main branch by default

- **WHEN** the objects page loads with no branch selected
- **THEN** the main branch's objects are listed

### Requirement: View an object's details

Selecting an object SHALL show its properties, relationships, and embedding status. Long free-form string properties SHALL be presented as usable multi-line fields, and client-side navigation into the detail view SHALL initialize those fields the same way a direct page load does.

#### Scenario: Open an object

- **WHEN** the user selects an object in the list
- **THEN** the object's properties and embedding status are shown

#### Scenario: Long text is readable

- **WHEN** an object's string property holds long text (for example tens of thousands of characters)
- **THEN** it renders as a multi-line, auto-growing, vertically resizable field sized to a comfortable reading height rather than a single-line box
- **AND** the field shows a live character count

#### Scenario: Client-side navigation initializes long-text fields

- **WHEN** the user navigates to an object's detail view by selecting a link (an htmx-boosted swap of the main content region) rather than loading the URL directly
- **THEN** the long-text fields are grown to their content and their character counts are initialized, exactly as on a direct page load

#### Scenario: Object has relationships

- **WHEN** the object has relationships
- **THEN** those relationships are listed with their type and source/target names

#### Scenario: Object has no relationships

- **WHEN** the object has no relationships
- **THEN** a clear "no relationships" state is shown

### Requirement: Surface load failures without crashing

The objects page SHALL show a clear error state when a backend fetch fails and SHALL NOT render a broken page.

#### Scenario: Backend unreachable

- **WHEN** a required backend request fails
- **THEN** the affected section shows an error message while the rest of the page remains usable

### Requirement: Search objects by text

The objects page SHALL let the user search objects by a text query in one of two modes — full-text or hybrid — and SHALL render ranked results with a relevance score. Type and branch filters SHALL ride along with the query so searching does not drop the active filters.

#### Scenario: Full-text search

- **WHEN** the user enters a query with the full-text mode selected
- **THEN** objects matching the query are returned from the full-text endpoint (`GET /api/graph/objects/fts`) and ranked by relevance

#### Scenario: Hybrid search

- **WHEN** the user enters a query with the hybrid mode selected
- **THEN** the query is auto-embedded and fused with lexical results (`POST /api/graph/search`), and the fused hits are ranked by relevance

#### Scenario: Search results are capped to the top 25

- **WHEN** a search matches more than 25 objects
- **THEN** only the top 25 ranked hits are rendered, and search mode offers no cursor pagination

#### Scenario: No matching objects

- **WHEN** a search matches nothing
- **THEN** a "No matching objects" empty state is shown with a clear-search escape hatch back to the browse list

### Requirement: Show project object and embedding stats

In browse mode the objects page SHALL show a stats row of three counts — total objects, pending embeddings, and failed embeddings — and SHALL render an unavailable state (not a misleading zero) when a stats source fails.

#### Scenario: Stats available

- **WHEN** the objects page loads in browse mode and the count and embedding-progress sources respond
- **THEN** the total object count (branch-scoped via `GET /api/graph/objects/count`) and the embedding-queue pending/failed counts (`GET /api/embeddings/progress`) are shown

#### Scenario: Stats unavailable

- **WHEN** a stats source fails
- **THEN** its tile renders an unavailable marker (`—`) rather than a zero value

#### Scenario: Stats hidden during search

- **WHEN** the user is viewing search results
- **THEN** the stats row is not shown (global counts never compete with a search's results)

### Requirement: Stable cursor pagination

Browse-mode pagination SHALL use a keyset cursor over `(created_at, id)` ordered descending with the unique `id` tiebreak, returning a `next_cursor` only when a further page exists; a forged or invalid cursor SHALL fail as a 400 (`invalid cursor`), never a 500.

#### Scenario: Cursor is a stable keyset

- **WHEN** a page is returned
- **THEN** the next cursor encodes the last row's `(created_at, id)`, and consecutive pages neither skip nor duplicate rows

#### Scenario: Final page

- **WHEN** the last page is returned
- **THEN** `next_cursor` is empty and no "Load more" control is rendered

#### Scenario: Invalid cursor

- **WHEN** a request carries a forged or malformed cursor
- **THEN** the backing list endpoint returns 400 with `invalid cursor`, and the gateway surfaces a load-failure state rather than a server error

#### Scenario: Pagination fetch failure

- **WHEN** the load-more request fails
- **THEN** the "Load more" control is retained so the user can retry, rather than being silently removed

### Requirement: Session-derived project with membership enforcement

The objects browser SHALL derive the project server-side from the signed session — picking only from the user's own projects with no client-supplied override — and SHALL forward it as `X-Project-ID` to backing endpoints that enforce project membership. This posture MUST be preserved: a future change SHALL NOT silently drop the membership enforcement behind the search, count, FTS, and hybrid-search endpoints.

#### Scenario: Project derived from the session

- **WHEN** a signed-in user opens the objects browser
- **THEN** the active project is resolved from the user's own project list (`resolveDefaultProject`), never from a client-supplied value

#### Scenario: Non-member with a foreign project

- **WHEN** a caller addresses a project whose owning organization they do not belong to
- **THEN** the backing endpoint returns 403 and no object data is disclosed

#### Scenario: Token bound to a different project

- **WHEN** a project-bound API token addresses a different project
- **THEN** the backing endpoint returns 403 on the token-binding mismatch

#### Scenario: Unknown project

- **WHEN** the addressed project does not exist
- **THEN** the backing endpoint returns 404

#### Scenario: Unauthenticated caller

- **WHEN** the backing endpoint is called without authentication
- **THEN** it returns 401

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
