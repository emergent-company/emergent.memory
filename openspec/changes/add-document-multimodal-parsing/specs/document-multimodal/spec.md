## Purpose

Persists the images extracted from documents and enriches them so that visual content — scanned pages and illustrations — becomes retrievable alongside the document text.

## ADDED Requirements

### Requirement: Persist extracted document images

The server SHALL store every image returned by extraction in the configured object storage as a document-scoped asset, recording its document, position/order, MIME type, and dimensions. Image references in the parsed content SHALL be rewritten to stable URLs served through an authenticated endpoint, and deleting a document SHALL delete its image assets.

#### Scenario: Images persisted and referenced

- **WHEN** a document is parsed and the extractor returns one or more images
- **THEN** each image is stored as a document asset and the parsed content references it by a stable URL

#### Scenario: Image references are authenticated

- **WHEN** a client requests a stored document image without valid credentials
- **THEN** the server returns an unauthorized response and no image bytes

#### Scenario: Document deletion removes images

- **WHEN** a document with image assets is deleted
- **THEN** its image assets are removed from storage

### Requirement: Route image enrichment by reported origin

The server SHALL enrich extracted images according to the origin reported by the extractor: full-page images from scanned pages SHALL be processed with an OCR prompt to recover text, and embedded illustrations SHALL be processed with a caption prompt to describe their content. The server SHALL use the OCR backend resolved by the extraction configuration and a configured vision-capable model. When no vision-capable model is available it SHALL skip captioning with a recorded warning and SHALL NOT fail the document.

#### Scenario: Scanned page uses OCR

- **WHEN** an extracted image is reported as a full page from a scanned document
- **THEN** the server OCRs it using the resolved OCR backend and stores the recovered text as the image's derived text

#### Scenario: Illustration uses captioning

- **WHEN** an extracted image is an embedded illustration
- **THEN** the server generates a caption and stores it as the image's derived text

#### Scenario: No vision model configured

- **WHEN** no vision-capable model is configured for the project
- **THEN** illustrations are persisted without derived text and a warning is recorded, and the document parse succeeds

### Requirement: Enriched image text is retrievable

The derived OCR text or caption of an image SHALL be injected into the document content inline, with an explicit marker identifying it as image-derived text attributed to its source image, so that chunking, search, and extraction over the document cover visual content. Image enrichment failures SHALL NOT remove or block the underlying image asset.

#### Scenario: Caption reaches chunks

- **WHEN** a document with an enriched illustration is chunked
- **THEN** the illustration's caption appears in the chunk content as image-derived text attributed to that image

#### Scenario: Failed enrichment does not block

- **WHEN** enrichment of one image fails
- **THEN** the remaining images and the document text are chunked and embedded normally, and the failed image is recorded as unenriched
