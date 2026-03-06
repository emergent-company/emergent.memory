## 1. Database & Infrastructure

- [x] 1.1 Create migration for `organization_provider_credentials` table (encrypted fields, gcp_project, location)
- [x] 1.2 Create migration for `organization_provider_model_selections` table
- [x] 1.3 Create migration for `project_provider_policies` table (enum: none, organization, project) and vertex metadata columns
- [x] 1.4 Create migration for `provider_supported_models` cache table
- [x] 1.5 Create migration for `llm_usage_events` table (project_id, multi-modal tokens, model, provider, cost)
- [x] 1.6 Create migration for `provider_pricing` table (text_input, image, video, audio, output, last_synced) and `organization_custom_pricing` table (org_id, model, text_input, image, video, audio, output)
- [x] 1.7 Add `LLM_ENCRYPTION_KEY` to internal config and implement AES-GCM encryption/decryption utility

## 2. Core Provider Domain Logic & Context Propagation

- [x] 2.1 Update `auth.Middleware` to ensure `ProjectID` and `OrgID` are consistently passed down via `context.Context` to all downstream layers
- [x] 2.2 Implement `ProviderRegistry` defining Google AI and Vertex AI provider types
- [x] 2.3 Implement credential resolution service following the hierarchy: Project -> Organization -> Env
- [x] 2.4 Implement model catalog service to fetch available models from APIs (with static fallback on timeout)
- [x] 2.5 Update configuration module to make `GOOGLE_API_KEY` and Vertex env vars optional fallbacks

## 3. LLM Client & Usage Tracking

- [x] 3.1 Refactor `adk.ModelFactory` and `genai.Client` injection from startup singletons to context-evaluating factories
- [x] 3.2 Update LLM and Embedding client factories to use resolved credentials and model selections
- [x] 3.3 Implement `UsageInterceptor` by wrapping the `google.golang.org/adk/model.LLM` interface
- [x] 3.4 Implement multi-modal token extraction and cost calculation resolving prices via `organization_custom_pricing` -> `provider_pricing`
- [x] 3.5 Implement a daily background cron job to sync the global `provider_pricing` table with an external registry
- [x] 3.6 Ensure RLS or application-level checks prevent cross-tenant credential access

## 4. API, SDK & CLI Implementation

- [x] 4.1 Create backend API endpoints for organization-level credential and model management
- [x] 4.2 Create backend API endpoints for project policy management and overrides
- [x] 4.3 Create backend API endpoints for usage and cost summary reporting
- [x] 4.4 Update Go SDK (`pkg/sdk/`) to consume the new provider and usage API endpoints
- [x] 4.5 Ensure backend LLM factory failures return actionable `4xx` error messages (e.g. "Run emergent provider set-key")
- [x] 4.6 Implement `emergent provider` CLI command group in `tools/emergent-cli` (set-key, set-vertex, models, usage)
- [x] 4.7 Extend `emergent projects` CLI with `set-provider` for policy management
- [x] 4.8 Update `emergent install` CLI command to print post-install instructions for configuring a provider if `--google-api-key` was not supplied
- [x] 4.9 Update `emergent projects create` CLI command to warn the user if no provider is currently configured for their organization

## 5. Testing & Validation

- [x] 5.1 Add unit tests for credential resolution hierarchy
- [x] 5.2 Add integration tests for model catalog fetching and static fallback
- [x] 5.3 Add E2E tests for project-level credential override flow
- [x] 5.4 Verify multi-modal token usage and cost calculation with test LLM calls