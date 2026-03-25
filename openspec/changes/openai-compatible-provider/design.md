## Context

Emergent Memory uses the Google ADK (`google.golang.org/adk`) as its agent execution framework. The `ModelFactory` in `pkg/adk/model.go` is the single point of LLM instantiation. It currently supports two backends: Google AI (Gemini API) and Google Cloud Vertex AI, both via the `google.golang.org/adk/model/gemini` driver.

The ADK defines a `model.LLM` interface with a single method: `GenerateContent(ctx, req, stream) iter.Seq2[*LLMResponse, error]`. Any struct implementing this interface can be used as a drop-in model. The ADK does **not** ship an OpenAI driver; the community adapter `github.com/huytd/adk-openai-go` exists but is immature and adds an unvetted dependency.

The credential resolution hierarchy (project → org → env vars) is already in place. The env-var fallback path in `ModelFactory.CreateModelWithName` is the natural insertion point for the new provider.

## Goals / Non-Goals

**Goals:**
- Support `openai-compatible` as a first-class DB-backed provider alongside `google` and `google-vertex`
- Store `base_url` (plain text) and `api_key` (encrypted) in `kb.org_provider_configs` / `kb.project_provider_configs` via a DB migration adding a `base_url` column
- Implement a minimal `openaiCompatibleModel` adapter in `pkg/adk` that speaks the OpenAI Chat Completions wire protocol
- Integrate into the full credential resolution hierarchy: project → org → env-var fallback
- Support `memory provider configure openai-compatible --base-url ... --api-key ... --model ...` in the CLI
- Support `memory provider test openai-compatible` (generative test only; no embedding test)
- Support `memory provider list` showing the configured base URL and model
- Register the model name in `kb.provider_supported_models` at configure time (no catalog API — user supplies the model name)
- Allow env-var fallback (`OPENAI_BASE_URL` + `OPENAI_API_KEY` + `LLM_MODEL`) for standalone/dev setups without DB credentials
- Make `LLMConfig.IsEnabled()` return true when `OPENAI_BASE_URL` is set

**Non-Goals:**
- Streaming support in the OpenAI adapter (ADK agents use non-streaming by default)
- Embedding support via OpenAI-compatible endpoints (embeddings stay Google-only; the embedding pipeline is separate)
- Automatic model catalog sync (no `/v1/models` enumeration — user specifies the model name explicitly)
- Cost tracking / usage events for the new provider (no pricing data for arbitrary local models)
- UI configuration in the admin panel (CLI + env-var only)

## Decisions

### Decision 1: Implement a minimal inline HTTP adapter rather than adding a third-party dependency

**Chosen:** Write a `openaiCompatibleModel` struct in `pkg/adk/openai_model.go` that uses `net/http` and `encoding/json` to call the `/v1/chat/completions` endpoint directly.

**Alternatives considered:**
- `github.com/sashabaranov/go-openai`: The canonical Go OpenAI client. Adds ~1,500 lines of dependency for a feature we only need ~100 lines of. Introduces a new transitive dependency chain and a third-party maintenance burden.
- `github.com/huytd/adk-openai-go`: Community ADK adapter. Immature, no releases, wraps `go-openai` anyway.

**Rationale:** The OpenAI Chat Completions API is extremely stable (unchanged since 2023). A minimal inline implementation is ~80 lines, has zero new dependencies, and is fully under our control. The ADK `model.LLM` interface is simple enough that the adapter is straightforward.

### Decision 2: Full DB-backed provider with env-var fallback

**Chosen:** Extend `OrgProviderConfig` / `ProjectProviderConfig` with a `base_url` column (plain text, nullable). The `api_key` is stored encrypted in the existing `encrypted_credential` column. Env-var fallback (`OPENAI_BASE_URL` + `OPENAI_API_KEY` + `LLM_MODEL`) is retained for standalone/dev setups.

**Alternatives considered:**
- Env-var-only for v1. Simpler, no migration. But means `memory provider configure`, `memory provider list`, `memory provider test`, and the full credential resolution hierarchy (project → org) don't work for OpenAI-compatible — it's a second-class citizen.

**Rationale:** The existing `OrgProviderConfig` entity already has `GCPProject` and `Location` as plain-text columns alongside the encrypted credential. Adding `base_url` follows the exact same pattern. A single nullable column migration is low risk. This gives OpenAI-compatible full parity: per-org/per-project overrides, `memory provider list`, `memory provider test`, and usage tracking all work out of the box.

### Decision 3: `LLM_MODEL` env var as the model name override

**Chosen:** Add `LLM_MODEL` as a new env var that overrides the model name when `OPENAI_BASE_URL` is set. The existing `VERTEX_AI_MODEL` env var remains the default for Google backends.

**Alternatives considered:**
- Reuse `VERTEX_AI_MODEL` for the model name. Confusing naming for non-Google backends.
- Use `OPENAI_MODEL` as the env var name. Less generic; `LLM_MODEL` signals provider-agnosticism.

**Rationale:** `LLM_MODEL` is the most intuitive name for a provider-agnostic model override. It matches the user's example in the feature request (`LLM_MODEL=kvasir`).

### Decision 4: ADK message format mapping

The ADK `LLMRequest` uses `google.golang.org/adk/model.Message` with `Role` and `Parts` (text/blob). The OpenAI API uses `{"role": "...", "content": "..."}`. The adapter maps:
- ADK `user` → OpenAI `user`
- ADK `model` → OpenAI `assistant`
- ADK `system` → OpenAI `system`
- Multi-part messages: concatenate text parts into a single `content` string (images/blobs are not supported in v1)

## Risks / Trade-offs

- **[Risk] Streaming not supported** → The adapter returns a single-item iterator. ADK agents use non-streaming `GenerateContent` calls, so this is acceptable for v1. If streaming is needed later, the adapter can be extended.
- **[Risk] Token usage not tracked** → `LLMUsageEvent` records are not written for OpenAI-compatible calls (no pricing data for arbitrary local models). Mitigation: log token counts from the response at DEBUG level.
- **[Risk] Response schema / structured output not supported** → `ExtractionGenerateConfigWithSchema` uses `genai.Schema` which is Google-specific. The OpenAI adapter ignores `ResponseSchema` and relies on JSON mode (`response_format: {type: "json_object"}`) instead. This may reduce extraction accuracy on models that don't support JSON mode. Mitigation: document the limitation.
- **[Risk] No embedding support** → The embedding pipeline is separate from the generative pipeline and stays Google-only. Users must still configure a Google provider for embeddings (semantic search, extraction). Mitigation: document clearly; `TestEmbed` returns "not supported" for `openai-compatible`.
- **[Risk] Model capability mismatch** → Local models may not support all features agents rely on (long context, function calling). This is a user responsibility; the adapter cannot validate model capabilities at configure time.
- **[Risk] DB migration adds nullable column** → `base_url` is nullable on both `kb.org_provider_configs` and `kb.project_provider_configs`. Existing Google rows are unaffected (column is NULL). Low risk.

## Migration Plan

1. Write Goose migration adding nullable `base_url TEXT` column to `kb.org_provider_configs` and `kb.project_provider_configs`
2. Add `BaseURL` field to `OrgProviderConfig` and `ProjectProviderConfig` Bun entities
3. Add `ProviderOpenAICompatible` constant and register in `registry.go`
4. Extend `extractPlaintext`, `buildTempResolvedCred`, `decryptOrgConfig`, `decryptProjectConfig` for the new provider
5. Add `BaseURL` + `IsOpenAICompatible` to `ResolvedCredential` (domain) and `adk.ResolvedCredential` (pkg)
6. Update `toADKCredential` adapter to map `BaseURL` through
7. Add `SyncModels` no-op path for `openai-compatible` (stores user-supplied model name directly)
8. Add `TestGenerate` OpenAI-compatible branch (HTTP call to `/v1/chat/completions`)
9. Add `TestEmbed` no-op for `openai-compatible` (returns "not supported" — non-fatal)
10. Add `openaiCompatibleModel` to `pkg/adk/openai_model.go`
11. Add OpenAI-compatible branch to `ModelFactory.CreateModelWithName` — DB-resolved first, env-var fallback second
12. Add env vars to `LLMConfig` for env-var fallback path
13. Update CLI: `memory provider configure openai-compatible`, `memory provider test`, `memory provider list`
14. Hot reload picks up Go changes; DB migration requires server restart

**Rollback:** Remove the `openai-compatible` org/project config via `memory provider configure openai-compatible --remove`. The migration column is nullable and backward compatible — no data loss on rollback.

## Open Questions

- Should `OPENAI_BASE_URL` env var take priority over DB-stored Google credentials, or only activate when no DB credential is found? (Current design: env var is checked first in `ModelFactory` before the DB resolver — explicit env config wins. This matches how `GOOGLE_API_KEY` env var currently works as a fallback but inverted for OpenAI since it's the new provider.)
- Should `LLM_MODEL` env var also override the model name for Google backends? (Current design: only applies when `OPENAI_BASE_URL` is set.)
