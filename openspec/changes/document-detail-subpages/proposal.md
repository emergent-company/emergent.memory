## Why

A document's detail view is a single long page: metadata, extraction trigger, the most recent extraction's objects/relationships, and every chunk stacked together. There is no way to see the document itself, and no history — a document extracted several times only ever shows the latest run, with no way to tell what each run produced, which staging branch it wrote, or whether that branch still needs merging.

## What Changes

- **Document detail becomes a sub-page area** with a left sub-navigation rail and three views:
  - **Overview** — metadata, extraction trigger, and a document preview.
  - **Chunks** — the existing chunk list, moved off the overview.
  - **Extraction history** — every extraction job for the document, newest first.
- **Document preview** renders the original file through a gateway proxy: PDFs inline, images inline, text/markdown as a capped text preview; other MIME types fall back to a download link.
- **Extraction history** lists each job with its timestamp, status, object/relationship counts, and discovered types. Each entry lazily loads its detail: the objects and relationships that run produced on its staging branch, and a **Merge into main** action for any branch still unmerged.
- The single "most recent extraction results" section is removed from the overview; history governs extraction review.

## Capabilities

### Modified Capabilities

- `document-browser`: document detail gains a sub-navigation rail, an original-file preview, a chunks sub-page, and a per-job extraction history with a merge action.

## Impact

- `apps/web-ui/gateway/documents.templ` — sub-nav, overview/preview, chunks and extraction-history pages.
- `apps/web-ui/gateway/ui.go` — page handlers and loaders for the new sub-routes.
- `apps/web-ui/gateway/main.go` — routes for the chunks/extractions sub-pages, the binary file proxy, and the merge action.
- `apps/web-ui/gateway/memory_documents.go`, `memory_branches.go` — gateway client for listing a document's extraction jobs, proxying the original file, reading merge readiness, and merging a branch.
- No server, schema, or migration changes: the gateway derives each job's staging branch from its created object IDs, reusing the existing graph endpoints.
