## Purpose

Extend the gateway objects browser (`GET /objects`) with text search, a project-health stats row, and cursor-paginated browse, while pinning the tenant-isolation contract the browser relies on: the project is derived server-side from the signed session and the backing `domain/graph` endpoints enforce project membership.

## MODIFIED Requirements

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

## ADDED Requirements

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
