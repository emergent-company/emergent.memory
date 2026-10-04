## Why

The parse stage discards content the extractor already returns: `xberg` returns `images` and `metadata`, but the worker reads only `content` and `tables`. Image bytes are never stored, illustration content (photos, figures, diagrams) is never described, and document metadata is never persisted. Scanned-page *text* is already OCR'd into `content`, so what is actually lost is the images themselves and the content of illustrations. WeKnora stores the images, routes full-page images to OCR and illustrations to a caption pass, and chunks the resulting text; Memory can do the same and reuse its configured models.

## What Changes

- Persist images returned by extraction as project-scoped document assets and rewrite image references in content to stable, authenticated URLs.
- Classify each image by origin using the **extractor-reported** origin (scanned page vs embedded illustration), then enrich accordingly: scanned pages are OCR'd, illustrations are captioned with a vision model.
- Inject derived image text into the document content inline, with an explicit marker, so it is chunked and retrievable; attribution of text to its source image lives on the image asset.
- Persist the metadata the extractor actually returns (page count, title, author, creation/modification, producer) plus the computed image count, in the document's existing metadata.
- Enrichment is best-effort: a failed OCR/caption never fails the document, and when no vision-capable model is configured enrichment is skipped with a recorded warning.

## Capabilities

### New Capabilities
- `document-multimodal`: persists extracted document images and enriches them (scanned-page OCR vs illustration captioning) so image content becomes retrievable.

### Modified Capabilities
- `document-extraction`: the server consumes extracted images and metadata (not only content/tables), and the extraction contract reports each image's origin and dimensions.

**Ownership note:** OCR backend/language/force *configuration resolution* and the extraction engine fallback chain are owned by `harden-document-pipeline`. This change consumes the resolved OCR backend; it does not define how it is configured.

## Impact

- `apps/server/domain/extraction/document_parsing_worker.go` (read `result.Images`/`result.Metadata`), `apps/server/pkg/xberg/client.go` (extend `ExtractedImage` with origin/dimensions), `apps/server/domain/documents/`.
- New `kb.document_images` table and object-storage writes; extraction metadata is stored in the existing `documents.metadata` (no new `kb.documents` columns).
- Depends on `harden-document-pipeline` for resolved OCR backend and engine; do not implement the OCR-config resolution here.
- Retrieval quality changes for image-heavy documents (caption/OCR text enters chunk content).
