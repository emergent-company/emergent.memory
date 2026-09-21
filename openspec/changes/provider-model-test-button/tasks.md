## 1. Memory API: explicit-model provider test

- [x] 1.1 Add `TestGenerateForModel` / `TestEmbedForModel` to `domain/provider/catalog.go`, factoring the generate path so it runs against an explicit model while `TestGenerate`/`TestEmbed` keep their exact behaviour.
- [x] 1.2 Accept an optional `{model, modelType}` JSON body on `TestProjectProvider` (`POST /api/v1/projects/:projectId/providers/:provider/test`); empty body → unchanged generate-then-embed behaviour; invalid `modelType` → 400. Update the route doc comment.
- [x] 1.3 Unit tests: generative override, embedding override, and a no-body regression test asserting `TestGenerate` still resolves the configured model.

## 2. Gateway client + handler

- [x] 2.1 Add `MemoryClient.TestProjectModel(ctx, provider, model, modelType)` and the `MemoryBackend` interface entry.
- [x] 2.2 Add route `POST /settings/providers/model-config/test` and handler `uiProjectDefaultModelTest`: read `model_type`, take the matching select value, split the provider prefix, call Memory, surface a toast. Reject unknown type, empty selection, and unprefixed model with a clear message.

## 3. Default models UI

- [x] 3.1 Add a quiet secondary Test button beside each default-model dropdown (`type="button"`, own `hx-post`, `hx-vals` model_type, `hx-include` the select, `hx-swap="none"`), with a busy spinner and `hx-disable` so it cannot be double-fired during the live call. Add `data-testid`s.
- [x] 3.2 Keep the existing save-on-change behaviour untouched and the rest of the page visually unchanged.

## 4. Tests

- [x] 4.1 Gateway handler tests: success generative, success embedding, empty selection, unprefixed model, unknown model_type, backend error surfaced.
- [x] 4.2 Render test asserting both dropdowns carry a Test button wired to the test endpoint.

## 5. Verification

- [x] 5.1 `go build ./...` + `go test ./domain/provider/... -count=1` from `apps/server`; pass.
- [x] 5.2 `templ generate` + `go build ./...` + `go test ./... -count=1` from `apps/web-ui/gateway`; pass.
- [x] 5.3 `golangci-lint run` on both changed areas; 0 issues.
- [ ] 5.4 Browser check on the running dev gateway: Test on each default-model dropdown reports success/failure; stored defaults unchanged.
