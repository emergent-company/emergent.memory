# Tasks

## 1. Protocol + auth types

- [x] 1.1 Add `Protocol` (`openai-chat`, `google-genai`, `anthropic-messages`) and `AuthStyle` (`bearer`, `api-key`, `x-api-key`, `x-goog-api-key`, `none`, `signed`) in `apps/server/domain/provider/protocol.go`. Verify `go build ./...`.
- [x] 1.2 Unit-test protocol/auth validation and the legacy vendor alias map (`google` → `gemini`; `google-vertex`/`openai`/`deepseek` unchanged). Verify `go test ./domain/provider/ -run Registry -count=1`.

## 2. Registry rework

- [x] 2.1 Extend `ProviderDefinition` with `Protocol`, `Auth`, `AuthByProtocol`, `DefaultBaseURLs`, `ModelTypes`, `URLPatterns`, `ExtraFields`, `CredentialLabel`, `CatalogStrategy`, `Compat`, `Order`, `Icon`; add `Protocol`/`AuthStyle`/`ExtraField`/`CredentialLabel` types. Verify `go build ./...`.
- [x] 2.2 Add `domain/provider/builtin.go` registering all target vendors as data (28 entries: openai, azure-openai, anthropic, deepseek, google, google-vertex, aliyun, zhipu, volcengine, hunyuan, siliconflow, minimax, moonshot, mimo, modelscope, qianfan, qiniu, longcat, lkeap, nvidia, novita, openrouter, requesty, litellm, generic, gpustack, jina, weknoracloud). Verify `go build ./...`.
- [x] 2.3 Add embedded SVG brand icons under `domain/provider/assets/` and set `Definition.Icon`. 27 WeKnora MIT-licensed SVGs embedded via `//go:embed`; every builtin definition carries a non-empty icon; exposed as `iconDataUri` from the definitions API.
- [x] 2.4 Unit-test registry completeness (every expected ID present, IDs unique, every entry has a protocol/auth). Verify `go test ./domain/provider/ -run Registry -count=1`.

## 3. Protocol adapters (`pkg/adk`)

- [x] 3.1 Replace `ResolvedCredential.IsGoogleAI`/`IsVertexAI` usage with `Protocol`, `Auth`, `Extra`; update `adk_adapter.go` conversion. Verify `go build ./...`.
- [x] 3.2 Add `pkg/adk/auth.go` with an injector for the five header styles plus a signed hook (signed returns a descriptive error); unit-test each style. Verify `go test ./pkg/adk/ -run Auth -count=1`.
- [x] 3.3 Add `pkg/adk/anthropic_model.go` implementing `model.LLM` against the Messages API (`x-api-key`, `anthropic-version`, mandatory `max_tokens`, text + tool_use/tool_result mapping). Unit-test request shaping and response parsing with `httptest`. Verify `go test ./pkg/adk/ -run Anthropic -count=1`.
- [x] 3.4 Update `pkg/adk/model.go` `CreateModelWithName` to dispatch by `cred.Protocol` and inject auth; keep the env-var fallback path working. Verify `go build ./...` and `go test ./pkg/adk/ -count=1`.
- [ ] 3.5 Make `openaiCompatibleModel` request shaping fully vendor-`Compat`-driven (max_tokens vs max_completion_tokens, thinking style). **Deferred** — the `Compat` field is populated per vendor but the adapter still keys the thinking/max-tokens behavior off model-name heuristics; DeepSeek/Qwen behavior is unchanged. Follow-up.

## 4. De-hardcode dispatch sites

- [x] 4.1 `domain/provider/service.go`: decrypt/validate credentials, base-URL defaults, provider/embedding selection via registry definitions. Verify `go test ./domain/provider/ -count=1`.
- [x] 4.2 `domain/provider/catalog.go`: `ResolveModels`, generate/embed paths, `buildClientConfig` dispatch by `CatalogStrategy`/`Protocol`. Verify `go test ./domain/provider/ -count=1`.
- [x] 4.3 `domain/provider/model_catalog_sync.go`: resync all OpenAI-compatible vendors. Verify `go test ./domain/provider/ -count=1`.
- [x] 4.4 `domain/extraction/usage.go`: map provider from the registry rather than a four-case switch. Verify `go test ./domain/extraction/ -count=1`.
- [ ] 4.5 `internal/config`: generalize provider env-var handling and `IsEnabled`/`UseVertexAI` onto the registry. **Deferred** — env-var-only configuration keeps its existing four-provider shape (unchanged behavior). Follow-up.
- [ ] 4.6 `pricing_sync.go`: add retail pricing rows for the new vendors. **Deferred** — pricing remains scoped to the four legacy vendors; per-vendor retail rows are a follow-up.

## 5. UI + CLI

- [x] 5.1 `apps/web-ui/gateway/settings_providers.go`: replace `providerWhitelist` with registry lookup; expose definitions via the server API. Verify `go build ./...` from `gateway/`.
- [x] 5.2 `project_settings.templ`: render registry vendors with names/descriptions, brand icons, and a read-only extra-fields panel. Verify `templ generate` + `go build ./...`.
- [x] 5.3 Unit-test the settings provider list is registry-driven and extra fields render. Verify `go test ./... -run Provider -count=1` from `gateway/`.
- [x] 5.4 `apps/cli/internal/cmd/provider.go`: `provider definitions` lists vendors from the registry. Verify `go build ./...` from `apps/cli/`.

## 6. Verification

- [x] 6.1 `go build ./...` from `apps/server`, `apps/web-ui/gateway`, `apps/cli`, `apps/server/pkg/sdk`; `go vet`.
- [x] 6.2 `task css generate` (templ + tailwind) and scoped `golangci-lint` on `domain/provider`, `pkg/adk`, `domain/extraction`; 0 issues in changed packages.
- [x] 6.3 Unit suites: `go test ./domain/provider/ ./pkg/adk/ ./domain/extraction/ -count=1` (server) and `go test ./gateway/... -count=1` (web-ui) all pass.
- [ ] 6.4 Browser test: Settings → Providers lists registry vendors; configure a new vendor. **Deferred to PR CI / manual** — no dev server available from the isolated worktree.
- [ ] 6.5 Update `openspec/specs/openai-compatible-llm` and `openspec/specs/provider-settings` on archive.

## 7. Out-of-scope follow-ups (file issues, do not implement)

- [ ] 7.1 File a GitHub issue for rerank protocol support.
- [ ] 7.2 File a GitHub issue for ASR/transcription protocol support.
- [ ] 7.3 File a GitHub issue for the OpenAI Responses protocol.
- [ ] 7.4 File a GitHub issue for vendor brand icons (task 2.3) and `Compat`-driven openai shaping (task 3.5).
