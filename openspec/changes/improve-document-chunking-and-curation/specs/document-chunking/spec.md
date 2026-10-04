## Purpose

Selects and applies chunking strategies that respect document structure and supports curating chunk content with revisions, so retrieval is not degraded by split tables, ignored headings, or broken code blocks.

## ADDED Requirements

### Requirement: Structure-aware chunking strategies

The server SHALL support selecting a chunking strategy per project. The available strategies SHALL include `recursive_character` (the current default) and a structure-aware strategy that splits on document structure such as markdown headings and paragraphs. When no strategy is configured, the server SHALL use `recursive_character` so existing behavior is preserved.

#### Scenario: Default strategy preserves behavior

- **WHEN** a project has no chunking strategy configured
- **THEN** the server chunks with `recursive_character`

#### Scenario: Structure-aware strategy respects headings

- **WHEN** a document written in markdown is chunked with the structure-aware strategy
- **THEN** chunks follow heading and paragraph boundaries rather than raw character offsets

#### Scenario: Invalid strategy rejected

- **WHEN** a project is configured with an unknown chunking strategy
- **THEN** the server rejects the configuration with a validation error

### Requirement: Project chunking configuration is validated on write

The server SHALL expose a write surface for a project's chunking configuration (strategy and parameters) and SHALL validate it on write, rejecting an unknown strategy or out-of-range parameters without persisting the configuration.

#### Scenario: Valid configuration persisted

- **WHEN** a client writes a valid chunking configuration for a project
- **THEN** the server persists it and subsequent chunking uses it

#### Scenario: Invalid configuration rejected

- **WHEN** a client writes a chunking configuration with an unknown strategy or out-of-range parameters
- **THEN** the server rejects it with a validation error and leaves the previous configuration unchanged

### Requirement: Protected segments are never split

The chunker SHALL keep fenced code blocks, block math, and image/link constructs whole, so they are never split across chunk boundaries. A table SHALL NOT be split when it fits within the maximum chunk size; an oversized table SHALL be split only at row boundaries, with its header carried into each part. When an atomic protected segment (code, math, or image/link) exceeds the maximum chunk size, it SHALL occupy its own chunk, and the chunk's metadata SHALL record that it is oversized. An oversized atomic chunk SHALL NOT be silently truncated; if it is too large for the embedding model it SHALL be flagged and skipped for embedding rather than failing the embedding job.

#### Scenario: Code block stays intact

- **WHEN** a document contains a fenced code block longer than the configured chunk size
- **THEN** the code block is emitted as a single chunk and is not split

#### Scenario: Table that fits stays intact

- **WHEN** a document contains a table that otherwise spans a chunk boundary but fits within the configured chunk size
- **THEN** the table is kept within one chunk

#### Scenario: Oversized table splits by row with header

- **WHEN** a table is larger than the configured chunk size
- **THEN** it is split only at row boundaries and each part includes the table's header row and separator

#### Scenario: Oversized atomic segment flagged

- **WHEN** a code or math block exceeds the maximum chunk size and the embedding model's context
- **THEN** the chunk is marked oversized in metadata and skipped for embedding rather than failing the job

### Requirement: Table headers are carried into chunks

When a chunk begins in the body of a table, the chunker SHALL prepend that table's header row and separator so the chunk includes column context. If the header cannot be determined, the chunk SHALL be emitted without a synthesized header and the chunk's metadata SHALL record `headerCarry: "unknown"`.

#### Scenario: Mid-table chunk gets header

- **WHEN** a table is large enough that a chunk starts at a body row
- **THEN** the emitted chunk begins with the table's header row and separator

#### Scenario: Unknown header is recorded

- **WHEN** a chunk starts mid-table and the header row cannot be determined
- **THEN** the chunk is emitted without an added header and its metadata records `headerCarry: "unknown"`

### Requirement: Parent-child chunking

The server SHALL support parent-child chunking, emitting parent chunks for document sections and child chunks within them, where each child records its parent. This mode SHALL be opt-in per project and off by default. Parent chunks SHALL be excluded from embedding and search by default; a consumer may expand a child to its parent for context.

#### Scenario: Child references parent

- **WHEN** a document is chunked with parent-child enabled
- **THEN** each child chunk records the identifier of its parent section chunk

#### Scenario: Disabled by default

- **WHEN** a project has not enabled parent-child chunking
- **THEN** chunks are emitted without parent identifiers

#### Scenario: Parents excluded from embedding and search

- **WHEN** parent-child chunking is enabled
- **THEN** parent chunks are not embedded and do not appear in search results unless explicitly requested

#### Scenario: Parent chunk text is bounded

- **WHEN** a section is larger than the parent-text size cap
- **THEN** the emitted parent chunk's text is truncated to the cap

### Requirement: Chunk curation with revisions

The server SHALL allow a chunk's text to be edited and SHALL record each change as a revision with the previous text and a monotonic revision sequence number, so an operator can view revision history, compute a diff between revisions, and roll back to a previous revision. An edit SHALL preserve the chunk's document provenance and SHALL cause the chunk to be re-embedded. Revision ordering SHALL be defined by the monotonic sequence number.

#### Scenario: Edit records a revision and re-embeds

- **WHEN** an operator edits a chunk's text
- **THEN** a new revision is recorded with the next sequence number and the chunk is scheduled for re-embedding

#### Scenario: Rollback restores prior text

- **WHEN** an operator rolls a chunk back to a previous revision
- **THEN** the chunk's text becomes that revision's text and the change is itself recorded as a new revision

#### Scenario: Revision history is ordered

- **WHEN** an operator lists a chunk's revisions
- **THEN** the revisions are returned by ascending sequence number with their text, author, and timestamp

#### Scenario: Original content is the first revision

- **WHEN** a chunk is created
- **THEN** its original text is recorded as revision sequence 1, so listing revisions always returns at least the original

#### Scenario: Skipped oversized chunk has no stale embedding

- **WHEN** an oversized chunk beyond the embedding context is skipped for embedding
- **THEN** any previously stored embedding is cleared so stale content is not returned by search

#### Scenario: Embedding for a superseded revision is discarded

- **WHEN** a chunk is edited or rolled back while its embedding is being generated
- **THEN** the embedding computed from the superseded text is discarded and a fresh embedding is generated for the current revision

### Requirement: Re-chunk preserves edited chunks

When a document is re-chunked, a chunk that was manually edited SHALL be preserved: it SHALL be retained with its text and flagged as edited, and a warning SHALL be reported when edited chunks are preserved. When the re-chunk is forced, edited chunks SHALL be discarded and a warning SHALL again be reported. The retention semantics SHALL define the preserved chunk's position and mark it stale when its source span no longer exists.

#### Scenario: Preserve edited chunk

- **WHEN** a document is re-chunked and a chunk had been manually edited, without force
- **THEN** the edited chunk is retained, flagged as edited, and a warning reports that edited chunks were preserved

#### Scenario: Force discards edits

- **WHEN** a document is re-chunked with force
- **THEN** edited chunks are discarded and a warning reports that manual edits were discarded

#### Scenario: Stale preserved chunk marked

- **WHEN** a preserved edited chunk's original source span no longer exists after re-chunk
- **THEN** the chunk is retained marked as stale rather than silently dropped
