## Why

Uploading documents and extracting structured information from them is the core ingestion path today, but the system treats every upload as a brand-new, unrelated document. There is no way to upload a revised version of an existing document, no way to see what changed between two revisions, and no way to feed those changes back into the knowledge graph without re-extracting the whole document and leaving stale objects behind.

This matters because the most common real-world workflow for living documents — a spec, a policy, a contract, a research note — is *repeated edits*, not one-shot uploads. Users currently work around this by deleting and re-uploading, which:
- destroys the link between a document and the graph objects it produced,
- regenerates every chunk and re-embeds identical content,
- re-runs full LLM extraction on unchanged text (slow, costly, non-deterministic),
- leaves graph objects from removed text in place, silently drifting the graph away from the source of truth.

The knowledge graph already has the primitives this needs — a version chain (`canonical_id`, `version`, `supersedes_id`, `change_summary`), a staging-branch + merge-and-review flow, and a per-object `content_hash`. Documents do not. This change closes the gap by giving documents a revision chain, a revision diff, and a review-gated path from a revision delta into the graph.

## What Changes

- **Document revision chain**: documents gain a logical identity (`document_group_id`) shared across revisions, plus `version_number`, `supersedes_document_id`, and an `is_current` flag with a per-group uniqueness guarantee. Existing documents are backfilled as single-revision groups. `parent_document_id` keeps its current meaning (hierarchy), unchanged.
- **Upload a revision**: a new endpoint takes a file and attaches it to an existing logical document, creating the next version in the chain rather than a standalone document.
- **Revision listing and discard**: list all revisions of a logical document and discard a non-current revision (removing its chunks and any staged graph objects).
- **Revision diff**: compare two revisions and return both a human-readable line diff of parsed content and a structured entity delta (added / updated / removed graph objects and relationships). Diff is computed against the staged extraction, not against the main graph.
- **Extraction provenance**: extraction records which chunks produced each graph object (`kb.object_chunks`, currently unused), which is what makes removed-content detection possible.
- **Review-gated graph update**: extraction for a revision lands on a staging branch and does **not** auto-merge. The user reviews the delta and applies it; applying merges the staged objects into the main graph and tombstones objects whose provenance is entirely removed chunks. Discarding drops the staging branch.
- **CLI + web UI**: `memory documents` gains revision subcommands; the document detail page gains a Revisions tab (list, diff, entity delta, apply / discard).

## Capabilities

### New Capabilities

- `document-revisions`: the logical document / revision-chain model, and the create-revision, list-revisions, and discard-revision HTTP API.
- `document-revision-diff`: content-level line diff and structured entity delta between two revisions.
- `document-revision-graph-apply`: revision extraction staging, the review gate, and the apply/discard path that updates the main graph (including removal of objects sourced only from removed content).
- `cli-document-revisions`: the `memory documents` CLI surface for revisions.
- `web-document-revisions`: the gateway web UI Revisions tab.

### Modified Capabilities

- `document-api`: upload semantics extended to support creating a revision, and the authenticated-surface requirement extended to cover the new revision endpoints.

## Impact

- **Database**: `apps/server/migrations/` — additive columns and a partial unique index on `kb.documents`; index on the revision chain; no destructive changes.
- **Backend — documents**: `apps/server/domain/documents/` — entity, repository, service, handler, routes (revision endpoints, `is_current` transitions, group resolution).
- **Backend — extraction**: `apps/server/domain/extraction/` — carry chunk identity through batch building and write `kb.object_chunks`; a per-job "do not auto-merge" mode for revision extraction; staging-branch lifecycle on discard.
- **Backend — diff**: new revision-diff service (text diff + entity-delta assembly), reusing `graph.BranchMergeReadiness` / `MergeBranch` dry-run.
- **Backend — graph**: `MergeBranch` apply path reused as-is; tombstoning of objects with fully-removed provenance may require a small graph-service addition.
- **SDK**: `apps/server/pkg/sdk/documents/` — revision methods.
- **CLI**: `apps/cli/internal/cmd/documents.go` — revision subcommands.
- **Web UI**: `apps/web-ui/gateway/documents.templ` (+ handlers) — Revisions tab.
- **Tests**: unit tests for the revision chain invariants, diff, and apply idempotency; integration test for the full revision cycle; optional e2e coverage.
- **No breaking API changes**: all new surfaces are additive; existing document endpoints keep their current behaviour for non-revision documents.

## Out of Scope (follow-up issues)

- Chunk-level incremental extraction (phase 2) — this change re-extracts the revision's changed regions but not a fully incremental chunk pipeline.
- Provenance backfill for graph objects created before this change (their source chunks cannot be reconstructed).
- A reaper for orphaned staging branches left by abandoned revisions.
