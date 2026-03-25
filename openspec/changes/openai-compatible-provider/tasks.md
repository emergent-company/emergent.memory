## 1. Database Migration

- [x] 1.1 Create Goose migration `apps/server/migrations/000XX_add_openai_compatible_provider.sql` adding nullable `base_url TEXT` column to `kb.org_provider_configs` and `kb.project_provider_configs`

## 2. Domain Entities

- [x] 2.1 Add `ProviderOpenAICompatible ProviderType = "openai-compatible"` constant to `apps/server/domain/provider/entity.go`
- [x] 2.2 Add `BaseURL string` field to `OrgProviderConfig` Bun entity with `bun:"base_url"` tag
- [x] 2.3 Add `BaseURL string` field to `ProjectProviderConfig` Bun entity with `bun:"base_url"` tag
- [x] 2.4 Add `BaseURL string` field to `UpsertProviderConfigRequest` JSON struct
- [x] 2.5 Add `BaseURL string` field to `ProviderConfigResponse` and `ProjectProviderConfigResponse` JSON structs
- [x] 2.6 Add `BaseURL string` and `IsOpenAICompatible bool` fields to `ResolvedCredential` in `apps/server/domain/provider/service.go`

## 3. Provider Registry

- [x] 3.1 Register `ProviderOpenAICompatible` in `apps/server/domain/provider/registry.go` with credential fields: `base_url` (required, non-secret) and `api_key` (optional, secret)

## 4. Credential Service

- [x] 4.1 Add `openai-compatible` case to `extractPlaintext` in `service.go` — require `BaseURL`; encrypt `APIKey` if provided (empty key is valid for keyless local servers)
- [x] 4.2 Add `openai-compatible` case to `buildTempResolvedCred` — populate `BaseURL`, `IsOpenAICompatible`, and `APIKey`
- [x] 4.3 Add `openai-compatible` case to `decryptOrgConfig` — set `BaseURL` from plain-text column, decrypt `APIKey` from ciphertext
- [x] 4.4 Add `openai-compatible` case to `decryptProjectConfig` — same as org
- [x] 4.5 Add `ProviderOpenAICompatible` to the provider loop in `ResolveAny` so it is tried alongside Google AI and Vertex AI

## 5. Model Catalog

- [x] 5.1 Add `openai-compatible` case to `SyncModels` in `catalog.go` — skip API fetch; directly upsert the user-supplied `GenerativeModel` name as a single `ModelTypeGenerative` entry in `kb.provider_supported_models`
- [x] 5.2 Add `openai-compatible` case to `TestGenerate` in `catalog.go` — make a direct HTTP POST to `{BaseURL}/chat/completions` with `{"model": ..., "messages": [{"role":"user","content":"Say hello in one sentence."}], "max_tokens": 64}`; return model name and reply text
- [x] 5.3 Add `openai-compatible` case to `TestEmbed` in `catalog.go` — return `"not supported"` as model name and `nil` error (embedding is not available for this provider; non-fatal)
- [x] 5.4 Add `openai-compatible` case to `buildClientConfig` in `catalog.go` — return an error ("use openai-compatible HTTP client directly") so any accidental genai client creation fails fast

## 6. ADK Credential Bridge

- [x] 6.1 Add `IsOpenAICompatible bool`, `OpenAIBaseURL string` fields to `adk.ResolvedCredential` in `apps/server/pkg/adk/credentials.go`
- [x] 6.2 Update `toADKCredential` in `apps/server/domain/provider/adk_adapter.go` to map `IsOpenAICompatible` and `BaseURL` from domain `ResolvedCredential` to `adk.ResolvedCredential`

## 7. OpenAI-Compatible Model Adapter

- [x] 7.1 Create `apps/server/pkg/adk/openai_model.go` with an `openaiCompatibleModel` struct implementing `model.LLM`
- [x] 7.2 Implement `openaiCompatibleModel.Name() string` returning the configured model name
- [x] 7.3 Implement ADK role mapping: `user`→`user`, `model`→`assistant`, `system`→`system`; concatenate multi-part text messages into a single `content` string
- [x] 7.4 Implement `openaiCompatibleModel.GenerateContent` to POST to `{baseURL}/chat/completions` with `model`, `messages`, and `max_tokens` fields
- [x] 7.5 Add `Authorization: Bearer {apiKey}` header when `apiKey` is non-empty
- [x] 7.6 Add JSON mode: include `response_format: {"type": "json_object"}` when the ADK request config has `ResponseMIMEType: "application/json"`
- [x] 7.7 Return descriptive errors for non-2xx HTTP responses (include status code and response body)
- [x] 7.8 Return descriptive errors for network failures (connection refused, timeout)
- [x] 7.9 Add `NewOpenAICompatibleModel(baseURL, apiKey, modelName string) model.LLM` constructor

## 8. ModelFactory Integration

- [x] 8.1 Add OpenAI-compatible branch in `ModelFactory.CreateModelWithName` (`apps/server/pkg/adk/model.go`) — when DB resolver returns a credential with `IsOpenAICompatible == true`, create an `openaiCompatibleModel` using `cred.OpenAIBaseURL`, `cred.APIKey`, and `resolvedModel`
- [x] 8.2 Add env-var fallback branch — when `f.cfg.OpenAIBaseURL != ""` and no DB credential was found, create an `openaiCompatibleModel` from env config; check this before the Google env-var paths
- [x] 8.3 Call `f.wrapModel(llm, "openai-compatible")` in both paths so the usage tracker wrapper is applied consistently
- [x] 8.4 Log `"creating model via OpenAI-compatible endpoint"` at DEBUG level with `baseURL` and `model` fields

## 9. Server Configuration (env-var fallback)

- [x] 9.1 Add `OpenAIBaseURL string` field to `LLMConfig` in `apps/server/internal/config/config.go` with env tag `OPENAI_BASE_URL` and empty default
- [x] 9.2 Add `OpenAIAPIKey string` field to `LLMConfig` with env tag `OPENAI_API_KEY` and empty default
- [x] 9.3 Add `OpenAIModel string` field to `LLMConfig` with env tag `LLM_MODEL` and empty default
- [x] 9.4 Update `LLMConfig.IsEnabled()` to return true when `OpenAIBaseURL != ""`
- [x] 9.5 Update the no-credentials error message in `ModelFactory` to mention `OPENAI_BASE_URL`+`OPENAI_API_KEY`+`LLM_MODEL` as a third option

## 10. CLI — provider configure

- [x] 10.1 Add `openai-compatible` to `ValidArgs` and the `switch` in `runProviderConfigure` in `tools/cli/internal/cmd/provider.go` — require `--base-url`, accept `--api-key` (optional) and `--model` (required)
- [x] 10.2 Add `--base-url` and `--model` flags to `configureCmd`
- [x] 10.3 Add `openai-compatible` to `ValidArgs` and the `switch` in `runProviderConfigureProject` — same flags
- [x] 10.4 Update `configureCmd` and `configureProjectCmd` Long descriptions to document the new provider with examples:
  ```
  memory provider configure openai-compatible --base-url http://localhost:11434/v1 --model llama3
  memory provider configure openai-compatible --base-url http://localhost:8001/v1 --api-key local --model kvasir
  ```

## 11. CLI — provider list

- [x] 11.1 Update `runProviderList` table output in `provider.go` to show `BaseURL` in place of `GCP PROJECT` column when provider is `openai-compatible` (or add a `BASE URL` column)

## 12. CLI — provider test

- [x] 12.1 Add `openai-compatible` to `ValidArgs` in `providerTestCmd`
- [x] 12.2 Update `runProviderTest` to include `openai-compatible` in auto-discovered providers from org configs

## 13. CLI — provider models

- [x] 13.1 Add `openai-compatible` to `ValidArgs` in `providerModelsCmd` so `memory provider models openai-compatible` works (returns the single configured model name from the catalog)

## 14. CLI — Installer

- [x] 14.1 Add `OpenAIBaseURL`, `OpenAIAPIKey`, `OpenAIModel` fields to `installer.Config`
- [x] 14.2 Add `--openai-base-url`, `--openai-api-key`, and `--llm-model` flags to `installCmd` in `tools/cli/internal/cmd/install.go`
- [x] 14.3 Update `GenerateEnvFile()` to write `OPENAI_BASE_URL`, `OPENAI_API_KEY`, and `LLM_MODEL` lines to `.env.local` when set
- [x] 14.4 Add `PromptLLMProvider()` to `Installer` — when neither Google nor OpenAI provider was supplied, offer a choice: Google AI, OpenAI-compatible, or skip; collect the relevant fields for the chosen option
- [x] 14.5 Update `Install()` to call `PromptLLMProvider()` instead of `PromptGoogleAPIKey()` when no provider was configured
- [x] 14.6 Update `printCompletionMessage()` to include OpenAI-compatible setup instructions alongside Google AI / Vertex AI guidance

## 15. CLI — config set / config show

- [x] 15.1 Add `"openai_base_url": "OPENAI_BASE_URL"`, `"openai_api_key": "OPENAI_API_KEY"`, and `"llm_model": "LLM_MODEL"` to `standaloneEnvKeys` in `tools/cli/internal/cmd/config.go`
- [x] 15.2 Update `memory config set` help text to document the three new keys with examples
- [x] 15.3 Update `memory config show` to display `OPENAI_BASE_URL` and `LLM_MODEL` from `.env.local` when set (mask `OPENAI_API_KEY` like the existing API key display)

## 16. CLI — doctor

- [x] 16.1 Update `checkGoogleAPIKey` in `tools/cli/internal/cmd/doctor.go` to also check for `OPENAI_BASE_URL` — show pass when either is configured, warn when neither is set
- [x] 16.2 When `OPENAI_BASE_URL` is set: show `✓ LLM Provider: openai-compatible (base_url=..., model=...)`
- [x] 16.3 When neither is set: mention both Google AI and OpenAI-compatible as options in the warning message

## 17. Tests

- [x] 17.1 Add unit tests in `apps/server/pkg/adk/openai_model_test.go` using `httptest.NewServer` — verify correct request format (model, messages, max_tokens, Authorization header)
- [x] 17.2 Add test for JSON mode: verify `response_format` is included when `ResponseMIMEType` is `application/json`
- [x] 17.3 Add test for role mapping: system/user/model → system/user/assistant
- [x] 17.4 Add test for multi-part message concatenation
- [x] 17.5 Add test for error handling: non-2xx response returns descriptive error
- [x] 17.6 Add test for `ModelFactory.CreateModelWithName` with DB-resolved `IsOpenAICompatible` credential: verify it creates an `openaiCompatibleModel`
- [x] 17.7 Add test for `ModelFactory.CreateModelWithName` with `OpenAIBaseURL` env-var fallback: verify it creates an `openaiCompatibleModel` without calling Google APIs
- [x] 17.8 Add test for `LLMConfig.IsEnabled()` returning true when `OpenAIBaseURL` is set
- [x] 17.9 Add test for `catalog.TestGenerate` with `openai-compatible` using `httptest.NewServer`

## 18. Documentation

- [x] 18.1 Update `apps/server/AGENT.md` env var reference to document `OPENAI_BASE_URL`, `OPENAI_API_KEY`, and `LLM_MODEL` with examples for Ollama, llama.cpp, and vLLM
