## Context

See `proposal.md` for motivation. Current state:

- `apps/server/pkg/xberg/client.go:92-97` returns `ExtractedImage{ Page, Data, MimeType }` (no origin, no dimensions). `ExtractMetadata` (`client.go:75-82`) returns `PageCount, Title, Author, CreationDate, ModificationDate, Producer`.
- `apps/server/domain/extraction/document_parsing_worker.go:395-410` consumes only `ExtractResult.Content` and `ExtractResult.Tables`; `Images` and `Metadata` are dropped. OCR is called on the whole document (auto-detect, `ForceOCR: false`), so scanned-page text already lands in `Content`.
- The pipeline is job-based: parse → chunk → embed → object/relationship extraction. Chunk text is the only searchable surface; there is no image storage or vision call.
- `documents.metadata` is an existing jsonb column; project/org model config resolves a generative model.

## Goals / Non-Goals

**Goals:**
- Images become first-class document assets whose derived text participates in retrieval.
- Enrichment is isolated from parse success and bounded in cost/latency.

**Non-Goals:**
- OCR backend/language/force configuration and the extraction engine fallback chain — owned by `harden-document-pipeline`.
- Native image/multimodal embeddings or a separate image vector space. Caption/OCR text enters the existing text chunk path.
- Per-page layout reconstruction or geometric reading-order analysis.
- A visual image-annotation or region-crop UI.

## Decisions

### Extend the extractor contract with image origin and dimensions

Add `origin` (scanned page vs embedded illustration) and pixel `dimensions` to the extracted-image contract and decode them in the xberg client. This is a real contract change, not an existing signal: today the extractor reports neither. When `origin` is absent the server treats the image as an illustration (the safe default: storing an image without diagnosing it as a scanned page).

- Why: routing OCR vs caption needs an explicit, testable signal; guessing from image size is unreliable without page geometry.
- Alternative: classify by image area vs page size — rejected (the extractor does not return page geometry per image, and the heuristic is fragile).

### Persist images as document assets

Store bytes under a document-scoped storage key and record an asset row (document id, order, MIME, dimensions, origin, derived text, enrichment status). Rewrite content `images/...` references to an authenticated asset URL.

- Why: keeps bytes out of Postgres, reuses the storage abstraction, cascades with document deletion.
- Alternative: base64 in a document column — rejected (row bloat, no reuse).

### Store extraction metadata in the existing document metadata

Persist the real `ExtractMetadata` fields and the computed `len(Images)` into `documents.metadata` (jsonb) rather than adding columns.

- Why: avoids a `kb.documents` migration that would collide with sibling changes and reuses an existing field.
- Alternative: typed columns — rejected (migration overlap, no query need identified).

### Inject derived text inline before chunking

Sequence: parse → persist images → enrich (bounded concurrency, per-image timeout) → inject derived text into content with an explicit `[image: …]`-style marker → chunk → embed. Attribution lives on the asset row; no new chunk-metadata field is introduced (chunk-metadata ownership belongs to `improve-document-chunking-and-curation`).

- Why: captions must exist before chunking to land in chunk text; inline markers avoid a cross-change metadata dependency.
- Alternative: a chunk-metadata image field — rejected (collides with the chunking change).
- Alternative: enrich after embedding and re-chunk — rejected (double chunk/embed cost).

### Consume, don't define, OCR configuration

The enrichment path reads the OCR backend resolved by `harden-document-pipeline`; it does not define how OCR settings are configured or resolved.

## Risks / Trade-offs

- [VLM cost/latency on image-heavy documents] → bounded per-image timeouts, bounded concurrency, per-document image cap, best-effort failures.
- [Caption hallucination entering retrieval] → derived text is marked as image-derived inline so citations can distinguish it from body text.
- [Storage growth] → document-scoped lifecycle and cascade delete; dedup by content hash within a document.
- [No vision model configured] → explicit skip + warning, document still parses.
- [Non-deterministic tests] → OCR/vision calls are behind an interface and mocked in unit tests.
- [Contract change to xberg] → the pinned xberg image must be updated to report origin/dimensions; when it does not, images default to illustration and still persist.

## Migration Plan

- Additive `kb.document_images` table; no `kb.documents` change (metadata goes to jsonb).
- Existing documents have no stored images until reparsed; reparse is owned by `harden-document-pipeline`.
- Rollback: enrichment is config-gated; asset tables are inert if unused.

## Open Questions

- Whether the per-document enriched-image cap should be a fixed default or project-configurable. Default conservative; revisit after cost data.
