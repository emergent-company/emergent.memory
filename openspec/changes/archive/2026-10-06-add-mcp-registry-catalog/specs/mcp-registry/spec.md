## Purpose

Replace the live upstream browse path for the MCP registry with an embedded,
curated catalog committed to the repo. The official registry
(`registry.modelcontextprotocol.io`) is slow and low-quality; instead of
tolerating it and caching it, Memory ships its own curated set in the same
`server.json` schema and serves browse + install from that snapshot with no
network dependency. The catalog is updated through pull requests.

## ADDED Requirements

### Requirement: Registry browse is served from an embedded curated catalog

Memory SHALL serve registry browse (search and single-server lookup) from an
embedded, curated catalog committed to the repository, using the official
`server.json` schema (`{"servers": [...]}`) so the same entry model is reused.
Browse SHALL have no network dependency: no HTTP request SHALL be made to the
official registry, and a browse request SHALL NOT fail because the upstream is
unreachable or slow. Search SHALL perform a case-insensitive substring match
over name, title, and description, ranking name matches before title matches
before description matches, and SHALL return entries in a deterministic order.
Because the catalog is small, pagination is unnecessary: the cursor is ignored
and no next page is produced.

#### Scenario: Search returns matching entries with no network call

- **WHEN** a client searches the registry with a query
- **THEN** matching entries are returned from the embedded catalog, ranked name-first, without contacting the official registry

#### Scenario: Lookup returns full detail with no network call

- **WHEN** a client requests a registry entry by its name
- **THEN** the full entry (packages, remotes, declared environment variables/headers) is returned from the embedded catalog

#### Scenario: Unknown entry

- **WHEN** a client requests a registry name that is not in the catalog
- **THEN** the API reports a not-found error rather than an empty entry

#### Scenario: No upstream failure to surface

- **WHEN** a browse request is served
- **THEN** there is no upstream call to fail; the only browse failure mode is a catalog that cannot be parsed, which SHALL fail startup rather than surface at request time

### Requirement: Registry install resolves from the embedded catalog

Memory SHALL resolve the server definition for a registry install from the
embedded catalog by the entry's registry name, instead of fetching it from the
official registry. Install SHALL keep the existing remote-only transport policy
(prefer `streamable-http` over `sse`; reject stdio-only packages) and the
existing SSRF guard on remote URLs. An install of a name absent from the
catalog SHALL report not-found without any network call.

#### Scenario: Install a catalog entry

- **WHEN** a user installs a registry entry that exists in the catalog and offers a remote transport
- **THEN** the definition is resolved from the catalog and a project MCP server is created for that URL

#### Scenario: Install of an unknown name

- **WHEN** a user attempts to install a registry name absent from the catalog
- **THEN** no server is created and the API reports not-found without contacting the upstream registry

### Requirement: The curated catalog is maintained via pull request

Memory SHALL treat the embedded catalog as a curated, version-controlled
artifact: entries SHALL be added, updated, or removed through pull requests
(and the associated CI) rather than at runtime or through any live sync. The
catalog SHALL remain in the official `server.json` schema so it can be
diffed against the live registry.

#### Scenario: Catalog changes flow through review

- **WHEN** the curated catalog needs a new or changed entry
- **THEN** the change is proposed via a pull request against the catalog file and merges through normal review, not at runtime

## REMOVED Requirements

### Requirement: Registry browse tolerates a slow upstream within a bounded budget

Memory SHALL give the official MCP registry a request budget long enough to
cover its observed slow-but-successful latency (~38.5s) while staying under the
gateway's 60s client timeout. A response that arrives within that budget SHALL
be returned to the caller rather than being reported as an upstream failure.

#### Scenario: Slow-but-successful upstream response is returned

- **WHEN** the official registry returns HTTP 200 only after ~38.5s
- **THEN** Memory returns the results instead of failing with a 502

#### Scenario: Upstream exceeds the budget

- **WHEN** the official registry does not respond within the bounded budget
- **THEN** Memory reports a clear upstream error and does not return an empty success

### Requirement: Repeated identical browse queries are served from a short-lived cache

Memory SHALL cache successful registry browse results (search and single-server
lookup) in memory, keyed by the full request identity — search keyed by query,
limit, and cursor; lookup keyed by name — for a short TTL (~60s). Within the
TTL, an identical query SHALL be served from the cache without re-hitting the
origin. Only successful results SHALL be cached.

#### Scenario: Identical search is served from cache

- **WHEN** a client repeats the same search (same query, limit, and cursor) within the TTL
- **THEN** the result is returned from the cache and the upstream registry is not called again

#### Scenario: Pagination hits the origin per cursor

- **WHEN** a client requests a different page (different cursor) of a search
- **THEN** Memory fetches that page from the upstream registry

#### Scenario: Transient failure is not sticky

- **WHEN** a browse request fails (upstream error) and then succeeds on a later retry
- **THEN** the error is not cached and the later retry returns the successful result

#### Scenario: Failure is still surfaced as an error

- **WHEN** the official registry is unreachable or returns an error
- **THEN** Memory reports the failure clearly and does not return a successful empty result
