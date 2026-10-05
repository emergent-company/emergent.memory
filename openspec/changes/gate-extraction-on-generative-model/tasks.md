<!-- openspec:archive-ready -->
## 1. Detection helper

- [x] 1.1 Add `errNoGenerativeModel` and `Server.extractionModelMissing(ctx)` in `documents.go`: fetch `GetEffectiveModelConfig`, treat a fetch error as not-blocked (capture via `captureError`), and return `mc != nil && mc.GenerativeModel == ""`. Verify: `go build ./...` compiles.

## 2. JSON API guard

- [x] 2.1 In `triggerExtraction`, return `409 Conflict` with `errNoGenerativeModel` before calling `CreateExtractionJob` when `extractionModelMissing` is true. Verify: `go test ./...` passes.

## 3. Web UI guard

- [x] 3.1 In `uiDocument`, compute `blocked := s.extractionModelMissing(ctx)` and thread it into `DocumentDetailPage`. Verify: `go build ./...` compiles.
- [x] 3.2 In `uiTriggerExtraction`, redirect with `errNoGenerativeModel` before enqueuing when blocked. Verify: `go test ./...` passes.
- [x] 3.3 Add `extractionBlocked`/`blocked` params to `DocumentDetailPage`/`extractionCard`; when blocked disable the button (via `Attrs`), swap the description, and render a warning alert plus a `/settings/providers` link. Verify: `templ generate` + `go build ./...` succeed.

## 4. Tests

- [x] 4.1 Update every `DocumentDetailPage(...)` call site for the new last arg. Verify: `go build ./...` compiles.
- [x] 4.2 Add render assertions: blocked render contains `disabled`, the warning text, and `href="/settings/providers"`; enabled render does not contain `disabled`. Verify: `go test ./...` passes.
- [x] 4.3 Add a blocked route test (`effectiveModel: &EffectiveModelConfig{}`) asserting `POST /documents/:id/extract` redirects with `?err=` and enqueues nothing. Verify: `go test ./...` passes.
- [x] 4.4 Rework the Playwright smoke test to configure a provider first and update the header comment. Verify: spec lints clean.

## 5. Verify

- [x] 5.1 `templ generate`, `go build ./...`, `go test ./...`, `task lint` all clean.
- [x] 5.2 `openspec validate gate-extraction-on-generative-model` passes.
