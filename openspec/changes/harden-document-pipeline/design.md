## Context

See `proposal.md` for motivation. Current state:

- `apps/server/domain/extraction/document_parsing_worker.go` routes bytes by `MimeType` + `Filename` to xberg, Whisper (audio), or a plain-text reader; `extractPlainText` (`:470-477`) does `string(content)` (UTF-8 assumed). `upload_handler.go:454-463` already sniffs (`http.DetectContentType`) but the worker does not re-detect at the routing boundary.
- `apps/server/pkg/xberg/client.go:112-125` `ExtractOptions` has `OCRBackend`/`OCRLanguage`/`ForceOCR`/`ExtractTables`/`ExtractImages`/`TimeoutMs` but **no engine field**; `ExtractResult.Metadata` has no engine. OCR is hardcoded in the worker (`:385-389`).
- `document_parsing_jobs.go` uses statuses `pending/processing/retry_pending/completed/failed/dead_letter`; `CancelJobsForDocument` (`:421-446`) sets `failed`, not `cancelled`. Jobs run in goroutines sharing one poll context; there is no per-job cancel signal.
- Documents store original bytes for uploads (`entity.go:38-41`, `storage_key`); raw-content documents created via `POST /api/documents` do not.
- Unused conversion surface: `entity.go:246-258` holds unused `RetryConversionResponse`/`CancelConversionResponse` types; `service.go:397-410` holds unused `ResetConversionStatus`/`MarkConversionNotRequired`, and `service.go:402-405` `MarkConversionFailed` is also unused (`MarkConversionCompleted` `:412-415` **is** used by `handler.go:234`).
- Sibling `document-versioning` models re-processing as a new revision row.

## Goals / Non-Goals

**Goals:**
- Survive dirty files where a repair or fallback exists; give operators reparse + visibility + stop.
- Keep the default parse path unchanged for well-formed files.

**Non-Goals:**
- Reimplementing format repair in Memory; office repair stays the extractor's job. Memory adds detection, routing, fallback, engine selection, and observable errors.
- A general document-processing plugin system: "engine" here means a selector the extractor exposes, not a new in-process parser framework.
- Rewriting the job queue.

## Decisions

### Define "engine" as an extractor-exposed selector, extending the contract

The extraction contract gains an `engine` request field and reports the engine used; Memory selects among the engines the extractor exposes and falls back through an ordered chain. This is a real contract change (the field does not exist today). If the extractor exposes no engine selection, the chain degrades to OCR-backend variants plus the plain-text path.

- Why: matches WeKnora's per-file-type engine routing without importing its sidecar, and keeps the abstraction honest about what exists.
- Alternative: claim engines already exist — rejected (they do not; `ExtractOptions` has no engine field).

### Format detection lives in Memory at the routing boundary

Memory detects the effective format before choosing the parse path, with an explicit OLE→legacy-Word mapping, because the failure mode is routing (a PDF renamed `.txt` reaching the plain-text reader), not xberg's internal format handling. Ambiguous signatures fall back to the declared extension.

- Why: Memory owns routing and already sniffs on upload.
- Alternative: rely on xberg internal sniffing — rejected (it never sees files misrouted to Whisper or the plain-text reader).

### Configurable extraction settings owned here

Project defaults → per-request/per-upload override → built-in defaults, for OCR backend/language/force and engine. This is the single owner of the behavior; `add-document-multimodal-parsing` consumes the resolved OCR backend.

### Stop via a real cancelled state and per-job cancel signal

Add a `cancelled` parse-job status, store a per-job `context.CancelFunc` keyed by job id, and cancel it on stop; the in-flight extractor call uses the per-job context (not the shared poll context). Stop is idempotent with completion.

- Why: flipping a DB status does not interrupt an HTTP call to the extractor; a signal is required.
- Alternative: mark `failed` as today — rejected (misrepresents a user action and the spec requires cancelled).

### Reparse reuses stored original bytes; versioning stays separate

Reparse loads stored original bytes and enqueues a parse job on the same path as first-parse. For versioned documents it updates the current revision's derived content; "create a new revision" is the versioning change's concern. Raw-text documents without bytes are rejected.

### Timeline bound to the parse job

Record stage events per parse job; the document endpoint resolves to the latest job and retains prior jobs.

## Risks / Trade-offs

- [Fallback masks a real failure] → every attempted engine is recorded and shown in the timeline.
- [Reparse cost/churn] → operator-initiated only.
- [Stop during extraction] → per-job context cancels; derived artifacts are written only after success, so discard is a no-op or a cleanup of the replaced attempt; the race with completion is idempotent.
- [Magic-number false positives] → detection overrides only when the signature is known and unambiguous.
- [Versioned-document reparse semantics] → stated explicitly; new-revision creation remains with `document-versioning`.
- [Non-deterministic tests] → extractor/cancel behavior behind interfaces and faked.

## Migration Plan

- Additive: `cancelled` parse-job status, parse-stage-events table, `engine` on the extract contract.
- No backfill; reparse is opt-in per document.
- Rollback: reparse/stop endpoints can be disabled; stage events are inert if unused.

## Open Questions

- Whether the engine fallback chain is fixed or project-configurable. Start fixed; revisit if operators need control.
