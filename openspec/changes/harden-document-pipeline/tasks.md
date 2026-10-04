## 1. Extraction settings (OCR + engine)

- [ ] 1.1 Add a unit test for resolving extraction settings (project default → upload/request override → built-in default), then implement resolution
- [ ] 1.2 Add a unit test that resolved settings and the engine selection are sent to the extractor and unsupported values are rejected, then implement
- [ ] 1.3 Add a unit test that available OCR backends/engines are enumerated with `easyocr` absent, then implement
- [ ] 1.4 Add a unit test that project-level extraction settings are persisted and validated on write, then implement the storage/validation surface
- [ ] 1.5 Extend the extract contract with an `engine` request field and engine-used reporting; verify with a contract fixture test

## 2. Format resilience at the routing boundary

- [ ] 2.1 Add a unit test that a PDF named `.txt` is detected by content and routed to the document parser, then implement routing-boundary detection
- [ ] 2.2 Add a unit test that an OLE container is mapped to legacy Word, then implement the explicit mapping
- [ ] 2.3 Add a unit test that an ambiguous signature falls back to the declared extension, then implement
- [ ] 2.4 Add unit tests for BOM, UTF-16/32, ambiguous encoding, and a legacy single-byte encoding, then implement encoding detection

## 3. Engine fallback chain

- [ ] 3.1 Add a unit test that a per-upload engine override is used over the project default, then implement
- [ ] 3.2 Add a unit test that a failing engine falls back to the next and records the failed attempts plus the final engine, then implement
- [ ] 3.3 Add a unit test that all-engines-fail names each attempted engine, then implement the error

## 4. Parse timeline and progress

- [ ] 4.1 Add a migration for parse-stage events keyed by parse job; verify with a migration test
- [ ] 4.2 Add a unit test that each stage records start/end/status/duration, then implement
- [ ] 4.3 Add a unit test that a failed stage records its error while prior stages remain, then implement
- [ ] 4.4 Add a unit test that reparse retains the prior job timeline and the document resolves to the latest job, then implement
- [ ] 4.5 Add a unit test that parse progress reports current stage and overall state, then implement

## 5. Reparse and stop

- [ ] 5.1 Add a migration adding the `cancelled` parse-job status; verify with a migration test
- [ ] 5.2 Add a unit test that reparse reuses stored original bytes with an optional override and enqueues a job, then implement
- [ ] 5.3 Add a unit test that reparse without original bytes is rejected, then implement the guard
- [ ] 5.4 Add a unit test that stopping a running parse cancels the per-job context, marks cancelled, and discards partial artifacts, then implement the cancel-signal plumbing
- [ ] 5.5 Add a unit test that a stop racing a completing parse is idempotent (completed result retained), then implement
- [ ] 5.6 Add a unit test that stopping a finished parse reports nothing-to-stop, then implement
- [ ] 5.7 Add a unit test that reparse of a versioned document updates the current revision without creating a new revision, then implement (coordinate with `document-versioning`)
- [ ] 5.8 Reconcile/remove the unused conversion surface: `RetryConversionResponse`/`CancelConversionResponse` types (`entity.go:246-258`) and the unused `ResetConversionStatus`/`MarkConversionNotRequired`/`MarkConversionFailed` helpers (`service.go`), keeping `MarkConversionCompleted`; verify with build + dead-code check

## 6. API, CLI, UI

- [ ] 6.1 Add unit tests for reparse, parse-status/timeline (latest job), and stop endpoints (success, not-found, validation), then implement handlers and routes
- [ ] 6.2 Add a unit test that an unauthenticated call to the new endpoints is rejected, then implement
- [ ] 6.3 Add CLI commands for reparse, parse-status, and stop; verify with command output tests
- [ ] 6.4 Add gateway parse progress/timeline, stop, and reparse controls; verify with render tests

## 7. Verification

- [ ] 7.1 `go build ./...` + `go vet ./...` + `go test ./...` in `apps/server` pass
- [ ] 7.2 `task lint` passes; `templ generate` produces no diff if gateway UI changed
- [ ] 7.3 Add an e2e test: upload a mislabeled file → observe timeline → stop an in-flight parse → reparse with a different engine
- [ ] 7.4 Manual browser test: upload a mislabeled file, confirm correct parsing; reparse with a different engine; watch the timeline; stop an in-flight parse
