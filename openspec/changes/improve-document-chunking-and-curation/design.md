## Context

See `proposal.md` for motivation. Current state:

- `apps/server/pkg/textsplitter/splitter.go:31-48` implements one recursive character splitter with separators `["\n\n", "\n", ". ", " "]`; defaults `1000`/`200` (`:13-18`). `apps/server/domain/chunking/service.go:122,143` hardcodes the strategy string `"recursive_character"` and supports only `{maxChunkSize, overlap}` per project (`:226-243`).
- `kb.chunks` (`apps/server/domain/chunks/entity.go:12-32`): `chunk_index`, `text`, `embedding vector(768)`, `tsv`, jsonb `metadata`. `ChunkMetadata` carries `Strategy`, `StartOffset`, `EndOffset`, `BoundaryType`; only `Strategy` is populated today (set to `"recursive_character"` at `chunking/service.go:143`). There is no parent reference. Chunk routes (`chunks/routes.go:21-29`) expose list **and four delete operations** — there is no update/edit route.
- Re-chunking is destructive delete+recreate (`chunking/service.go:174-196`); the embedding worker (`extraction/chunk_embedding_worker.go:372`) embeds chunk text verbatim with no truncation.
- `projects.chunking_config` is a `map[string]any` (`projects/entity.go:24`) with a typed `ChunkingConfig{Strategy …}` struct (`:49-54`) kept "for future use"; there is **no writer or validation path** for chunking config.
- Chunk embedding is a re-enqueueable queue/worker.

## Goals / Non-Goals

**Goals:**
- Improve retrieval quality on structured documents without changing the default chunking behavior.
- Make bad chunks correctable without losing the ability to re-derive chunks.
- Keep the change inside the existing chunk/embed pipeline.

**Non-Goals:**
- Semantic/embedding-based chunk boundary detection (requires an extra embedding pass per document).
- A general-purpose document editor or chunk reordering.
- CLI chunk commands (deferred).
- Changing the embedding dimension or vector store.

## Decisions

### Strategy registry with a back-compat default + validated write surface

Introduce a chunking-strategy interface and registry. Register `recursive_character` (existing algorithm) as default and add a structure-aware strategy. Add the missing project chunking-config write/validation surface (endpoint + validation against the registry); unknown strategy or out-of-range parameters are rejected before persisting.

- Why: preserves existing behavior while enabling per-project choice; today there is no writer at all, so "rejected on write" requires building it.
- Alternative: mutate the existing splitter in place — rejected (silent behavior change for every project).

### Structure-aware splitting via a protected-segment tokenizer

Tokenize into protected segments (fenced code, tables, block math, images, links) and plain text with a markdown-aware scan. Protected segments are atomic; plain text is recursively split. Oversized protected segments become their own chunk, flagged in metadata, and skipped for embedding if they exceed the model context (never truncated, never a hard failure).

- Why: protects the constructs that most damage retrieval, without coupling to the embedding worker's failure mode.
- Alternative: full AST parse — heavier than needed for boundary decisions.

### Header carry in the chunker

Track the most recent table header while walking table segments; prepend it when a chunk begins in table body rows. When unknown, do not fabricate one; record `headerCarry: "unknown"` in chunk metadata.

### Parent-child as an opt-in overlay; parents excluded from search

When enabled, section headings become parent chunks and contained chunks are children recording the parent. Parents are **not embedded and not searched** by default; a consumer may expand a child to its parent. This is a behavioral decision with cost implications and is specified as such.

### Append-only chunk revisions with a monotonic sequence

Add `kb.chunk_revisions` (chunk id, monotonic `seq`, text, author, timestamp). A chunk records its current revision. Creating a chunk writes sequence 1 (the original); editing and rollback both append. The migration backfills sequence 1 for every existing chunk from `kb.chunks.text` so the "original revision" contract holds. Revision ordering is by `seq`, not timestamp.

- Why: matches the provenance/version philosophy; timestamp alone has no total order.
- Alternative: in-place update with no history — rejected (no audit/rollback).

### Re-chunk preservation semantics

Re-chunk retains manually edited chunks, flagged `edited`, and re-inserts them when their original source span still exists; when the span is gone the chunk is retained marked `stale`. A warning is reported on every re-chunk that preserves edited chunks. `force` discards them with a warning. This makes the divergence from a pure function of source explicit and reversible.

- Why: destructive delete+recreate loses curation; the review flagged that "preserve" needed concrete position/staleness semantics.
- Alternative: always discard on re-chunk — rejected (loses curation).

## Risks / Trade-offs

- [Default behavior regression] → characterization test pins current splitter output before refactor.
- [Oversized protected segment] → own chunk, flagged, skipped for embedding if over context; never truncated.
- [Parent-child doubles rows] → opt-in; parents excluded from embed/search.
- [Preserved edited chunks diverge from source] → `edited`/`stale` flags + warning + `force` escape hatch.
- [Re-embedding cost after edits] → only the edited chunk is re-enqueued.
- [New config write surface] → validated against the registry; invalid writes leave prior config intact.

## Migration Plan

- Additive: `kb.chunks` gains `parent_chunk_id`, `edited`, `current_revision`; `kb.chunk_revisions` created; backfill sequence 1 per existing chunk from `kb.chunks.text`.
- No re-chunk required; projects keep the default strategy.
- Rollback: strategies/parent-child off by config; revisions inert if unused.

## Open Questions

- Whether the structure-aware strategy should become the default in a later change once measured (deferred pending retrieval evaluation).
