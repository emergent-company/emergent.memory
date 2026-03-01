## 1. Database & Infrastructure

- [ ] 1.1 Create migration for `organization_provider_credentials` table (encrypted fields, gcp_project, location)
- [ ] 1.2 Create migration for `organization_provider_model_selections` table
- [ ] 1.3 Create migration for `project_provider_policies` table (enum: none, organization, project) and vertex metadata columns
- [ ] 1.4 Create migration for `provider_supported_models` cache table
- [ ] 1.5 Create migration for `llm_usage_events` table (project_id, multi-modal tokens, model, provider, cost)
- [ ] 1.6 Create migration for `provider_pricing` table (text_input, image, video, audio, output, last_synced) and `organization_custom_pricing` table (org_id, model, text_input, image, video, audio, output)
- [ ] 1.7 Add `LLM_ENCRYPTION_KEY` to internal config and implement AES-GCM encryption/decryption utility

## 2. Core Provider Domain Logic & Context Propagation

- [ ] 2.1 Update `auth.Middleware` to ensure `ProjectID` and `OrgID` are consistently passed down via `context.Context` to all downstream layers
- [ ] 2.2 Implement `ProviderRegistry` defining Google AI and Vertex AI provider types
- [ ] 2.3 Implement credential resolution service following the hierarchy: Project -> Organization -> Env
- [ ] 2.4 Implement model catalog service to fetch available models from APIs (with static fallback on timeout)
- [ ] 2.5 Update configuration module to make `GOOGLE_API_KEY` and Vertex env vars optional fallbacks

## 3. LLM Client & Usage Tracking

- [ ] 3.1 Refactor `adk.ModelFactory` and `genai.Client` injection from startup singletons to context-evaluating factories
- [ ] 3.2 Update LLM and Embedding client factories to use resolved credentials and model selections
- [ ] 3.3 Implement `UsageInterceptor` by wrapping the `google.golang.org/adk/model.LLM` interface
- [ ] 3.4 Implement multi-modal token extraction and cost calculation resolving prices via `organization_custom_pricing` -> `provider_pricing`
- [ ] 3.5 Implement a daily background cron job to sync the global `provider_pricing` table with an external registry
- [ ] 3.6 Ensure RLS or application-level checks prevent cross-tenant credential access

## 4. API, SDK & CLI Implementation

- [ ] 4.1 Create backend API endpoints for organization-level credential and model management
- [ ] 4.2 Create backend API endpoints for project policy management and overrides
- [ ] 4.3 Create backend API endpoints for usage and cost summary reporting
- [ ] 4.4 Update Go SDK (`pkg/sdk/`) to consume the new provider and usage API endpoints
- [ ] 4.5 Implement `emergent provider` CLI command group in `tools/emergent-cli` (set-key, set-vertex, models, usage)
- [ ] 4.6 Extend `emergent projects` CLI with `set-provider` for policy management

## 5. Testing & Validation

- [ ] 5.1 Add unit tests for credential resolution hierarchy
- [ ] 5.2 Add integration tests for model catalog fetching and static fallback
- [ ] 5.3 Add E2E tests for project-level credential override flow
- [ ] 5.4 Verify multi-modal token usage and cost calculation with test LLM calls