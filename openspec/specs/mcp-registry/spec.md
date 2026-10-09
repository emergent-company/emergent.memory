# mcp-registry Specification

## Purpose
Harden the official MCP registry browse path against a slow or transiently
degraded upstream: tolerate latency within a bounded budget, serve repeated
identical browse queries from a short-lived cache, and still surface failures
as errors rather than an empty success.

## Requirements

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

### Requirement: Catalog refresh supports GitHub and official-registry sources

The catalog refresh tool SHALL build the embedded catalog from a curated source
list that can mix two source kinds: a GitHub MCP-registry slug (resolved at
`{github-base}/{slug}?format=json`) and an official MCP registry server name
(line prefixed `official:`, resolved at
`{registry-base}/v0.1/servers/{name}/versions/latest`). Each source SHALL yield
a full official `server.json` server object with its `_meta` removed and a
non-empty `name`, and the tool SHALL preserve source-list order. The tool SHALL
fail without writing on any source that 404s or lacks a name.

#### Scenario: Official-registry source is included

- **WHEN** the source list contains `official:io.github.homeassistant-ai/ha-mcp`
- **THEN** the refresh tool fetches that server from the official MCP registry and adds its server object (minus `_meta`) to the catalog

#### Scenario: Mixed sources preserve order

- **WHEN** the source list mixes GitHub slugs and `official:` names
- **THEN** the resulting catalog contains every entry in source-list order

#### Scenario: Failing source aborts the refresh

- **WHEN** any GitHub slug or official name cannot be fetched or has no name
- **THEN** the tool exits non-zero and leaves the existing catalog unchanged
