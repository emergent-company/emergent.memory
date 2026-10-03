## Context

Provider handling is spread across `domain/provider` (credentials, catalog, pricing), `pkg/adk` (model construction/execution), `domain/extraction` (usage), `internal/config` (env), the web-ui gateway, and the CLI. The unifying defect is that vendor identity is a closed enum (`ProviderDialect`) and is switched on at every layer. `pkg/adk` already has a generic OpenAI-compatible client (`openaiCompatibleModel`) and a Google genai path, so the runtime primitives exist; only the dispatch is hardcoded.

Reference implementation studied: Tencent WeKnora `internal/models/providers/` — `definition.go` (`Definition`, `AuthStyle`, `ExtraField`, `CredentialLabel`, `Endpoint` hook), `builtin.go` (27 vendors), and per-protocol compat structs (`internal/models/api`). We adopt the same shape, reduced to Memory's chat + embedding scope.

## Goals / Non-Goals

**Goals**
- Adding a vendor is one registry entry (plus optional icon/pricing), no switch edits.
- Vendor identity and wire protocol are separate concepts.
- All former switch sites read from one registry.
- Existing four identifiers and stored rows keep working (alias map).
- Chat + embeddings for the full WeKnora vendor set.

**Non-Goals**
- Rerank and ASR/transcription protocols (deferred).
- OpenAI Responses protocol (OpenAI keeps Chat Completions).
- Org-level provider config (deprecated).
- Removing `ProviderDialect` entirely (kept as vendor ID type + alias map).

## Decisions

### D1 — `Protocol` and `AuthStyle` are orthogonal to vendor ID
```go
type Protocol string // "openai-chat" | "google-genai" | "anthropic-messages"
type AuthStyle string // "bearer" | "api-key" | "x-api-key" | "x-goog-api-key" | "none" | "signed"
```
`Definition.Protocol` selects the client; `Definition.Auth` selects credential injection. `AuthByProtocol` overrides auth for a vendor exposing a second protocol (e.g. Gemini native vs OpenAI facade).

### D2 — `Definition` is data; adapters consume it
`Definition` carries `ID`, localized `Name`/`Description`, `Icon []byte`, `Protocol`, `Auth`, `DefaultBaseURLs map[ModelType]string`, `ModelTypes`, `URLPatterns`, `ExtraFields`, `CredentialLabels`, `Compat`, `Endpoint func(...)`, `Order`. A `Builtins() []*Definition` slice is the registry source; `Registry` indexes it by ID.

### D3 — Auth injection is a shared helper
`pkg/adk.AuthInjector.Apply(*http.Request, AuthStyle, credential)` sets `Authorization: Bearer`, `api-key`, `x-api-key`, `x-goog-api-key`, nothing, or delegates to a signer hook. The genai SDK path receives auth via `genai.ClientConfig` for Google; Anthropic/OpenAI use the injector.

### D4 — Keep `ProviderDialect` as the vendor ID type
`ProviderDialect` is already stored in `kb.*provider_configs.provider`. We keep it as the vendor-id string type and add constants for the new vendors. `isDialectName`/alias resolution maps legacy `google` → `gemini`.

### D5 — Catalog fetch strategy lives on the definition
`Definition.CatalogStrategy` ∈ `openai-models` | `google-genai` | `static`. `ResolveModels` switches on this instead of the vendor ID.

### D6 — Extra config fields are stored in a per-config JSON/`extra` map
Azure `api-version`, LKEAP `secret_key`, Volcengine `region`/`thinking_type` are persisted alongside the credential and surfaced through `ExtraFields` metadata for the UI. Where the existing schema has no column, the value is folded into the encrypted credential JSON payload.

### D7 — Existing rows keep working
No DB migration: `provider` is free text. An alias map translates `google` → `gemini` at resolution/read time; `google-vertex`, `openai`, `deepseek` are unchanged. New vendor IDs resolve directly.

## Risks / Trade-offs

- **Anthropic Messages is a new client** (streaming/tool-call round-trip). Mitigation: implement non-streaming block response + tool_use/tool_result mapping, mirroring `openaiCompatibleModel`; unit-test with httptest.
- **Azure v1 vs dated endpoint**: the `Endpoint` hook reproduces WeKnora's rule (v1 default, dated only when `api-version` set).
- **27 vendors is a large data surface**: entries are declarative and mostly `openai-chat` + bearer; a registry completeness test guards IDs.

## Migration Plan

1. Land types + registry + adapters with the existing four vendors represented as entries (behavior unchanged), verified by the existing provider/ADK test suites.
2. Add the remaining vendors as data.
3. Replace switch sites one file at a time, keeping tests green.
4. UI/CLI read the registry.
No data migration; rollback is reverting the PR.

## Open Questions

- Whether to expose `ExtraFields` in the API response now or only via the UI. Decision: include in the definitions endpoint so the UI is fully dynamic.
