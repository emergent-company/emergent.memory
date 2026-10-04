## Why

Real-world uploads are dirty and parsing is opaque. Extension is trusted over content, so a PDF renamed `.txt` is misrouted; office files with repairable packaging or merged-cell defects can fail; non-UTF-8 text mojibakes; and there is no way to select or fall back between extraction engines. OCR is hardcoded to `tesseract`/`eng`. Once a document is parsed there is no way to re-run parsing with different settings (only extraction retry), and operators cannot see where a slow parse is stuck or stop it. WeKnora's resilience engineering (magic-number correction, package repair, parser fallback chains) and its stage-by-stage parse timeline show what to harden.

## What Changes

- Make extraction settings configurable and resolved consistently: OCR backend (`tesseract` | `paddleocr` | `sceptre` | `vlm`), language(s), force-OCR, and the extraction engine, from project defaults overridden per-request/per-upload. This change is the single owner of extraction-settings resolution and engine-used recording.
- Extend the extraction contract to accept an engine selection and to report which engine produced the result.
- Detect the effective format by content at the routing boundary (not only by name), with an explicit mapping for legacy binary formats (e.g. OLE → legacy Word), so a PDF renamed `.txt` is not sent to the plain-text reader.
- Add a parser-engine fallback chain: try engines in order when one produces no content, record every attempted engine on partial fallback, and name all attempted engines when all fail.
- Tolerate repairable office packaging and merged-cell defects; detect text encoding including BOM and UTF-16/32.
- Add reparse/reprocess from a document's stored original bytes with an optional new configuration, without re-uploading.
- Add parse observability: a parse-job stage timeline with progress, and a stop operation backed by a real cancelled state and cancel signal.

## Capabilities

### New Capabilities
- `document-parser-resilience`: corrects mislabeled formats at the routing boundary, selects and falls back between extraction engines, and tolerates repairable file defects.
- `document-pipeline-observability`: records and exposes per-parse-job stage progress bound to the parse job, and supports stopping an in-flight parse with a cancelled state.

### Modified Capabilities
- `document-extraction`: OCR becomes configurable (backend, language, force) and an extraction engine is selectable and reported, instead of the hardcoded `tesseract`/`eng`.
- `document-api`: adds reparse, parse-status/timeline, and stop-parse operations.
- `document-browser`: adds parse progress/timeline display, a stop control, and a reparse control.

## Impact

- `apps/server/pkg/xberg/client.go` (engine field + engine metadata), `apps/server/domain/extraction/document_parsing_worker.go` (routing-boundary detection, fallback chain, stage recording, cancel signal), `document_parsing_jobs.go` (cancelled status).
- `apps/server/domain/modelconfig`/project config for extraction settings; a new parse-stage-events table and a `cancelled` parse-job status.
- Documents store original bytes for uploads (`storage_key`); reparse requires them.
- Coordination: `add-document-multimodal-parsing` consumes the resolved OCR backend and does not define configuration; reparse-versus-revision interaction is stated against the `document-versioning` model.
- Gateway document detail UI and CLI document commands.
