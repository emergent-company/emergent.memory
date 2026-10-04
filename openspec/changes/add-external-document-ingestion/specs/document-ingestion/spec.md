## Purpose

Ingests documents from external locations — a web URL or a connected source identified by provider and external id — with safe fetching, deduplication, change detection, and re-sync.

## ADDED Requirements

### Requirement: Ingest a document from a URL

The server SHALL ingest a document from a supplied URL by fetching its content, extracting text through the existing extraction pipeline, and storing a new document whose `source_url` records the origin. Fetching SHALL enforce a maximum response size and a request timeout (configurable, with defaults), and SHALL reject loopback, private, link-local, and cloud-metadata targets including any reached by a redirect.

#### Scenario: URL ingested

- **WHEN** a client requests ingestion of a reachable public URL
- **THEN** the server fetches the page, extracts its text, and returns a document whose `source_url` is the requested URL

#### Scenario: SSRF target rejected

- **WHEN** a client requests ingestion of a URL that resolves to a private or loopback address
- **THEN** the server rejects the request with a validation error and does not fetch the target

#### Scenario: Redirect to a private target rejected

- **WHEN** a public URL redirects to a private, loopback, link-local, or cloud-metadata address
- **THEN** the server re-validates the redirect target, aborts the fetch, and returns an error without storing a document

#### Scenario: Oversized or slow response aborted

- **WHEN** a fetched response exceeds the size limit or the request exceeds the timeout
- **THEN** the server aborts the fetch, removes any partially fetched bytes, and returns an error without storing a partial document

#### Scenario: Non-success HTTP status fails cleanly

- **WHEN** the target returns a non-success HTTP status
- **THEN** the server returns an error identifying the status and creates no document

#### Scenario: Unsupported content type

- **WHEN** the target returns a content type the extraction pipeline cannot parse
- **THEN** the server rejects the ingestion with an error and creates no document

#### Scenario: Unreachable URL fails cleanly

- **WHEN** a client requests ingestion of a URL that cannot be reached
- **THEN** the server returns an error and creates no document

### Requirement: Normalize URLs for source identity

For URL ingestion the server SHALL derive the source identity from a normalized form of the URL — lowercasing the scheme and host, removing a default port and the fragment — so that equivalent URLs of the same page map to the same source. The normalized URL SHALL be used as the `source_external_id` under the built-in `web` provider.

#### Scenario: Equivalent URLs deduplicate

- **WHEN** two URLs that differ only by host case, default port, or fragment are ingested
- **THEN** the second ingestion updates the existing document rather than creating a second document

### Requirement: Ingest a document from an external source

The server SHALL ingest a document supplied by an external source, identified by a provider and an external id, recording both on the document. Ingesting the same `(provider, external_id)` SHALL update the existing document rather than create a duplicate.

#### Scenario: First ingestion creates a document

- **WHEN** a source item with a new `(provider, external_id)` is ingested
- **THEN** the server creates a document recording the provider and external id

#### Scenario: Repeated ingestion updates in place

- **WHEN** a source item whose `(provider, external_id)` already exists is ingested again
- **THEN** the server updates the existing document and does not create a second document

#### Scenario: Concurrent duplicate ingestion deduplicated

- **WHEN** the same source item is ingested concurrently
- **THEN** exactly one document exists for that `(provider, external_id)`

### Requirement: Detect source changes and re-sync

The server SHALL detect whether re-ingested source content has changed by comparing a hash of the fetched source bytes against the stored `source_content_hash`, and when it has changed SHALL update the stored bytes, content, and hash and mark the document for re-extraction. When content is unchanged the server SHALL update the source's last-synced time without re-extracting.

#### Scenario: Changed content triggers re-extraction

- **WHEN** a source item is re-ingested with bytes whose hash differs from the stored `source_content_hash`
- **THEN** the server updates the stored bytes, content, and hash, and schedules re-extraction

#### Scenario: Unchanged content is a no-op

- **WHEN** a source item is re-ingested with bytes whose hash matches the stored `source_content_hash`
- **THEN** the server updates the last-synced time and does not re-extract or duplicate the document

#### Scenario: URL refresh re-fetches the source

- **WHEN** a client refreshes a document that has a `source_url`
- **THEN** the server re-fetches the URL and applies the change-detection rules

### Requirement: Source ingestion is project-scoped and authenticated

All ingestion operations SHALL require the same authentication boundary as the existing gateway endpoints and SHALL be scoped to the active project, and MUST NOT ingest into or read from another project.

#### Scenario: Unauthenticated request

- **WHEN** a caller invokes an ingestion operation without valid credentials
- **THEN** the server returns an unauthorized response and ingests nothing

#### Scenario: Cross-project isolation

- **WHEN** a client ingests a source identified by an external id that exists in a different project
- **THEN** the server creates or updates a document in the caller's active project only
