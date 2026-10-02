## ADDED Requirements

### Requirement: Sources are created, read, updated, and deleted

A source SHALL be a durable, project-scoped record carrying a `type`, a `config` (transport/connection settings), an `auth` (credential reference), and a `sync_state` (last cursor/status). A user SHALL be able to create, read, update, and delete sources.

#### Scenario: Create a source with auth

- **WHEN** a user creates a source with a type, config, and auth credential reference
- **THEN** the source is persisted project-scoped and can be listed

#### Scenario: Delete a source

- **WHEN** a source is deleted
- **THEN** its sync jobs and state SHALL be removed, and the source no longer appears in listings

#### Scenario: Auth is a credential reference, not a raw secret

- **WHEN** a source is created
- **THEN** its auth SHALL reference a stored credential (per `configuration-management` / provider credential handling), not store a raw token inline

### Requirement: Incremental sync with state and cursor

A source sync SHALL be incremental: it SHALL persist a sync state/cursor (e.g. last-synced external id, watermark, or etag) and SHALL resume from that cursor on the next sync rather than re-reading the entire source.

#### Scenario: Cursor advances after sync

- **WHEN** a sync completes successfully
- **THEN** the source's persisted cursor SHALL advance to reflect the last processed item

#### Scenario: Next sync resumes from cursor

- **WHEN** a subsequent sync runs
- **THEN** it SHALL begin from the persisted cursor and only fetch items after it

#### Scenario: Cursor persists across restarts

- **WHEN** the server restarts
- **THEN** the persisted cursor SHALL survive and the next sync resumes from it

### Requirement: Sync job lifecycle

A sync SHALL be a job with a lifecycle of `pending`, `processing`, `completed`, `failed`, and `cancelled` (plus `dead_letter` for poison items), each transition SHALL be recorded, and a cancelled sync SHALL stop consuming source items.

#### Scenario: Job transitions through lifecycle

- **WHEN** a sync is triggered
- **THEN** it SHALL enter `pending`, then `processing`, then `completed`, `failed`, or `cancelled`, with each transition recorded

#### Scenario: Cancel stops a running sync

- **WHEN** a running sync is cancelled
- **THEN** it SHALL transition to `cancelled` and stop fetching further source items

### Requirement: Idempotent re-sync

Re-running a sync over already-ingested content SHALL be idempotent: it SHALL NOT create duplicate documents, chunks, or graph objects for content already ingested under the same source identity.

#### Scenario: Re-sync produces no duplicates

- **WHEN** a source is re-synced and no new content exists
- **THEN** no duplicate documents, chunks, or graph objects SHALL be created

### Requirement: Errors are surfaced per sync

A sync failure SHALL record a structured error (message, item/context, timestamp) on the job and SHALL be surfaced via the source status, so the user can see what failed without inspecting logs.

#### Scenario: Failed sync records error

- **WHEN** a sync fails on an item
- **THEN** the job SHALL record a structured error and the source status SHALL reflect the failure

#### Scenario: Partial failure does not lose the cursor

- **WHEN** a sync fails partway
- **THEN** successfully ingested items remain, the cursor SHALL reflect the last successfully processed item, and the next sync resumes from there

### Requirement: Ingested content lands in documents, chunks, and graph

Content ingested from a source SHALL flow through the existing pipeline — `domain/documents` (create/update document) → chunking (`kb.chunks`) → `domain/extraction` → `domain/graph` — with each piece attributable to its source.

#### Scenario: Source content becomes documents and chunks

- **WHEN** a source sync ingests an item
- **THEN** a document SHALL be created (or updated) and chunked through the existing chunking path

#### Scenario: Extracted graph objects are attributable to the source

- **WHEN** extraction runs over ingested content
- **THEN** the resulting graph objects SHALL be attributable to the source that produced them

### Requirement: MCP registry ProxyManager is the connector bus

The preferred transport for a source SHALL be an MCP server/tool registered via `domain/mcpregistry` and invoked through its `ProxyManager` (`CallToolOnServer`/`CallTool`), so that consuming a SaaS source SHALL NOT require a vendor SDK hard-coded into the server. `domain/mcprelay` (the inbound WebSocket star relay for NAT'd local connectors) SHALL be used only for OS-level local-node connectors, not SaaS ingestion.

#### Scenario: Source configured via MCP

- **WHEN** a source declares an MCP server/tool as its transport
- **THEN** ingestion SHALL be performed by invoking that tool through `domain/mcpregistry`'s `ProxyManager`, not by a bespoke server-side vendor client

#### Scenario: Relay is not the SaaS path

- **WHEN** a source targets a SaaS provider
- **THEN** it SHALL be reached through `mcpregistry`'s `ProxyManager`, and SHALL NOT depend on `mcprelay` (which requires a live local `(projectID, instanceID)` session)

### Requirement: Schema auto-discovery on new sources

When a new source is created, schema/type discovery SHALL be delegated to the existing `discoveryjobs` mechanism rather than a new parallel discovery subsystem.

#### Scenario: Discovery delegated to discoveryjobs

- **WHEN** a new source is created
- **THEN** schema/type auto-discovery SHALL be scheduled through `discoveryjobs`

### Requirement: Hand-written connectors are limited to highest-value targets

This change SHALL deliver the ingestion framework, NOT a fleet of hand-written connectors. Any first-class (non-MCP) connector SHALL be limited to the 1-2 highest-value targets and SHALL be documented as future work.

#### Scenario: Framework-first, not connector breadth

- **WHEN** this change lands
- **THEN** the ingestion framework SHALL be complete and no more than the documented 1-2 first-class connectors SHALL be planned as future work, not implemented here
