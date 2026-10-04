## Why

Chunking is a single recursive character splitter (default 1000/200). Tables split across chunk boundaries and lose their header row, headings are ignored, and code/math blocks can be cut mid-structure — all of which degrade retrieval. There is also no way to correct a bad chunk: the only lever is destructive `recreate-chunks`, which deletes and re-inserts every chunk. WeKnora's splitter protects structural segments and re-prepends table headers into chunks that start mid-table, and its chunk editing/revision model lets operators fix content. Memory already models provenance and versions, so it can do the same.

## What Changes

- Add structure-aware chunking: markdown/heading-aware splitting, protected segments (tables, code fences, math, links/images) that are never split, and header carry that re-prepends a table's header into a chunk starting mid-table.
- Add optional parent-child chunking (section → chunk) with a parent reference, where parent chunks are excluded from embedding/search by default.
- Add per-project chunking configuration (strategy + parameters) with a validated write surface, and preserve today's recursive-character behavior as the default.
- Add chunk curation: edit a chunk's text, revision history with a monotonic sequence, diff, and rollback, with edits triggering re-embedding and preserving document provenance.
- Define re-chunk preservation semantics for manually edited chunks (preserve by default, discard on force with a warning).

## Capabilities

### New Capabilities
- `document-chunking`: selects and applies structure-aware chunking strategies (protected segments, heading awareness, table-header carry, parent-child) with validated per-project configuration and chunk curation.

### Modified Capabilities
- `document-api`: adds chunk edit, revision-history, diff, rollback, and chunking-configuration read/write operations.
- `document-browser`: adds a chunk editor with revision history and rollback.

## Impact

- `apps/server/domain/chunking/`, `apps/server/domain/chunks/`, `apps/server/pkg/textsplitter/`, `apps/server/domain/projects` (chunking config write/validation).
- New chunking strategies and a parent reference on `kb.chunks`; new `kb.chunk_revisions`; migration with a revision backfill.
- Chunk edits enqueue re-embedding through the existing embedding queue.
- Gateway chunk preview becomes an editor. (CLI chunk commands are deliberately out of scope in this change.)
- Retrieval behavior changes for structured documents (better table/section recall).
