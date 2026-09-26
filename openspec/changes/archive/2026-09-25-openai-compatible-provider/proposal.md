## Why

Emergent Memory currently only supports Google AI (Gemini API) and Google Cloud Vertex AI as LLM providers, which requires a Google account and cloud credentials. Self-hosters running local inference servers (llama.cpp, Ollama, LM Studio, vLLM) or using OpenAI-compatible APIs cannot use the platform at all. Adding an OpenAI-compatible provider via env-var configuration unlocks a large segment of self-hosted and privacy-conscious users with zero additional infrastructure requirements.

## What Changes

- Add `OPENAI_BASE_URL`, `OPENAI_API_KEY`, and `LLM_MODEL` environment variables to `LLMConfig` for configuring an OpenAI-compatible provider
- Add a new `openai-compatible` provider type that uses the OpenAI wire protocol to call any compatible endpoint
- Implement a custom `model.LLM` adapter in `pkg/adk` that wraps the standard `net/http` client to speak the OpenAI Chat Completions API (`/v1/chat/completions`)
- Extend `ModelFactory.CreateModelWithName` to detect and use the OpenAI-compatible backend when configured
- Extend `LLMConfig.IsEnabled()` to return true when `OPENAI_BASE_URL` is set
- Add `openai-compatible` as a recognized `ProviderType` in the provider registry (env-var-only; no DB credential storage required for the initial implementation)
- Update the error message in `ModelFactory` to mention the new env vars

## Capabilities

### New Capabilities
- `openai-compatible-llm`: Support for any OpenAI-compatible LLM endpoint as an env-var-configured provider, enabling local inference (Ollama, llama.cpp, LM Studio, vLLM) and any third-party OpenAI-compatible API

### Modified Capabilities
- None — existing Google AI and Vertex AI paths are unchanged

## Impact

- **`apps/server/internal/config/config.go`**: Add `OpenAIBaseURL`, `OpenAIAPIKey` fields to `LLMConfig`; update `IsEnabled()` and `UseVertexAI()` logic
- **`apps/server/pkg/adk/model.go`**: Add OpenAI-compatible branch in `CreateModelWithName`; add `openaiModel` struct implementing `model.LLM`
- **`apps/server/pkg/adk/credentials.go`**: Add `IsOpenAICompatible`, `OpenAIBaseURL`, `OpenAIAPIKey` fields to `ResolvedCredential`
- **`apps/server/domain/provider/entity.go`**: Add `ProviderOpenAICompatible` constant
- **`apps/server/domain/provider/registry.go`**: Register the new provider type with its credential fields
- **No DB migration required** for the initial env-var-only implementation
- **No breaking changes** — all existing Google AI / Vertex AI paths are unaffected
- **Dependency**: Requires adding `github.com/sashabaranov/go-openai` (the standard Go OpenAI client) to `go.mod`, or implementing a minimal HTTP client inline to avoid the dependency
