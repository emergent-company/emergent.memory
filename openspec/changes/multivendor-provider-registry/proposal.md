## Why

Memory supports exactly four LLM provider types (`google`, `google-vertex`, `openai`, `deepseek`). The provider type is a closed Go enum that simultaneously names a *vendor* and a *wire protocol*, and that enum is switched on in ~11 places (credential decrypt, base-URL defaults, model catalog fetch, model dispatch, usage mapping, pricing, config, UI whitelist, CLI). Adding a vendor — e.g. Qwen, Zhipu, Anthropic, Azure OpenAI, OpenRouter, Ollama, vLLM — means editing every one of those switch sites.

The market standard (Tencent WeKnora, `internal/models/providers/`) is a **data-driven provider registry**: one struct per vendor describing protocol, auth style, default base URL, model types, extra config fields, URL patterns, and protocol compatibility quirks; a small set of protocol adapters consumes it. Adding a vendor is then one data entry.

Memory already has most of the runtime required: the generic `pkg/adk/openaiCompatibleModel` speaks Chat Completions for any endpoint, and the Google genai path already exists. What is missing is (a) vendor identity decoupled from protocol, (b) an auth-injection layer, and (c) a registry that is the single source of truth consumed by every former switch site.

## What Changes

- Introduce a `Protocol` (wire protocol) and `AuthStyle` (credential injection) vocabulary independent of vendor identity.
- Replace the four-entry `ProviderDefinition` registry with a data-driven `Builtins()` registry of all target vendors, each carrying localized name/description, embedded SVG icon, protocol, auth style, default base URLs per model type, supported model types, URL patterns, extra config fields, and protocol compatibility defaults.
- Add protocol adapters: keep `openai-chat` and `google-genai`; add `anthropic-messages`. `openai-chat` covers the majority of vendors (including Azure OpenAI via auth + endpoint hooks).
- Replace hardcoded provider switches in `domain/provider/{service,catalog,pricing_sync,model_catalog_sync}.go`, `pkg/adk/{model,credentials}.go`, `domain/extraction/usage.go`, `internal/config`, the web-ui provider whitelist, and the CLI with registry lookups.
- Make model catalog fetch strategy vendor-driven (`openai /models` | genai list | static).
- Keep the existing four provider identifiers working via an alias map (`google` → `gemini`, `google-vertex` unchanged).
- **Deferred (not in this change)**: rerank and ASR/transcription protocols; OpenAI Responses protocol (OpenAI continues to use Chat Completions); per-vendor native pricing ingestion beyond static rows.

## Capabilities

### New Capabilities
- `multivendor-provider-registry`: a data-driven catalog of LLM/embedding vendors (protocol, auth, base URLs, extra fields, compatibility), consumed by all provider dispatch paths, so new vendors are added as data rather than switch-case code.

### Modified Capabilities
- `openai-compatible-llm`: the OpenAI-compatible runtime becomes one protocol among several, selected by the vendor definition rather than by the `openai`/`deepseek` string prefix; adds Anthropic Messages as a second protocol.
- `provider-settings`: the project settings provider surface is generated from the registry, including per-vendor extra configuration fields and branding.

## Impact

**Server (`apps/server/`)**
- `domain/provider/`: `entity.go` (Protocol/AuthStyle), `registry.go` (data-driven `Definition` + `Builtins`), new `builtin.go` + `assets/` icons, `service.go` (credential decrypt/validation via definition), `catalog.go` (fetch/generate/embed dispatch via definition), `pricing_sync.go`, `model_catalog_sync.go`, `adk_adapter.go`.
- `pkg/adk/`: `credentials.go` (`Protocol`/`Auth`/`Extra` instead of `IsGoogleAI`/`IsVertexAI`), new `anthropic_model.go`, `model.go` (dispatch by protocol + auth injector), `openai_model.go` (compat-driven request shaping).
- `domain/extraction/usage.go`: vendor-aware usage recording.
- `internal/config/`: provider env vars generalized.

**Web UI (`apps/web-ui/gateway/`)**
- `settings_providers.go` (whitelist → registry), `project_settings.templ` (dynamic extra fields + icons).

**CLI (`apps/cli/`)**
- `internal/cmd/provider.go` reads the registry.

**Data**
- No schema change required: `kb.*provider_configs.provider` is free text with unique-key constraints, not a CHECK enum. Existing `google` rows alias to `gemini`.
