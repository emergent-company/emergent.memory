## 1. Extractor contract: image origin and dimensions

- [ ] 1.1 Add a unit test that the xberg client decodes `origin` and `dimensions` from the extracted-image payload, then extend `ExtractedImage` and the decoder
- [ ] 1.2 Add a unit test that an image without a reported origin is classified as an embedded illustration, then implement the default
- [ ] 1.3 Update the pinned xberg image / contract expectation so the origin field is reported; verify with an extractor-contract fixture test

## 2. Consume images and metadata

- [ ] 2.1 Add a unit test asserting the parse worker persists `ExtractMetadata` fields and the computed image count into `documents.metadata`, then implement
- [ ] 2.2 Add a unit test asserting absent metadata fields do not fail the parse, then implement
- [ ] 2.3 Add a unit test asserting `ExtractResult.Images` is handed to the image pipeline instead of discarded, then implement the hand-off

## 3. Image asset storage

- [ ] 3.1 Add a migration for `kb.document_images`; verify with a migration test
- [ ] 3.2 Add a unit test for storing an extracted image as a document-scoped asset (key, MIME, dimensions, origin, order), then implement
- [ ] 3.3 Add a unit test asserting content `images/...` references are rewritten to stable asset URLs, then implement
- [ ] 3.4 Add a unit test asserting document deletion cascades to image assets, then implement
- [ ] 3.5 Add a unit test asserting the image-serving endpoint rejects unauthenticated requests, then implement the authenticated endpoint

## 4. Image enrichment

- [ ] 4.1 Add a unit test that a scanned-page image routes to OCR and an illustration routes to the caption path, then implement origin-based routing
- [ ] 4.2 Add a unit test that captioning uses the resolved vision-capable model and stores the caption as derived text, then implement (mock the model)
- [ ] 4.3 Add a unit test that no vision-capable model configured skips illustration captioning with a warning and does not fail the document, then implement the guard
- [ ] 4.4 Add a unit test that the OCR path uses the backend resolved by the extraction configuration (no hardcoded backend here), then implement the consumption
- [ ] 4.5 Add a unit test that a single image enrichment failure records status and continues the document, then implement best-effort isolation with timeouts and bounded concurrency

## 5. Retrieval integration

- [ ] 5.1 Add a unit test asserting derived image text is injected inline with an identifying marker before chunking and lands in a chunk, then implement injection
- [ ] 5.2 Add a unit test asserting a document with a failed enrichment still chunks and embeds its text and remaining images, then implement

## 6. Verification

- [ ] 6.1 `go build ./...` + `go vet ./...` + `go test ./...` in `apps/server` pass
- [ ] 6.2 `task lint` passes
- [ ] 6.3 Manual browser test: upload a scanned PDF and an image-heavy document; confirm images render, OCR/caption text appears as image-derived text in chunk preview, and search returns the document
