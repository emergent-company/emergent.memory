## Why

LLM provider credentials (Google AI API key, Vertex AI service account) are currently configured globally via environment variables, making per-organization and per-project isolation impossible. Multi-tenant deployments need each organization to bring their own credentials, projects need flexible override policies, and operators need to understand which models are available and what usage costs they are incurring. The distinction between Google AI (API key, simpler) and Vertex AI (service account, location-aware, enterprise) also needs to be first-class — they behave differently and expose different model catalogs.

## What Changes

- Define two concrete providers: **Google AI** (API key → Gemini models) and **Vertex AI** (service account JSON + GCP project + location → Gemini/PaLM on Vertex)
- Add organization-level credential storage (encrypted at rest) per provider — one credential set per provider per organization
- For Vertex AI credentials, also store: GCP project ID and region/location (e.g. `us-central1`)
- Per-provider, store a list of **supported models** (fetched from the provider's API at credential-save time and cached in the database); separate lists for: embedding models and generative models
- Add per-provider **model selection** at organization level: default embedding model, default generative model (used by agents)
- Add project-level key policy per provider with three options:
  - **`none`** (default) — project uses whatever the server environment provides
  - **`organization`** — project inherits the organization's credential and model selections
  - **`project`** — project has its own credential + model overrides independent of the organization
- Backend resolves effective credential + model per request: project-own → organization → server env fallback
- Track **LLM usage and estimated cost** per project: token counts (text/image/video/audio input, output), model used, and cost estimate derived from multi-modal provider pricing tables stored in the backend; expose via API and CLI
- Add CLI commands for full credential and policy management via `emergent-cli`

## Capabilities

### New Capabilities

- `llm-provider-config`: Backend provider registry defining Google AI and Vertex AI as first-class provider types; organization-level credential storage (encrypted); model catalog fetching from provider APIs; credential resolution hierarchy (project → org → env)
- `project-provider-policy`: Per-project, per-provider policy (`none` / `organization` / `project`), optional project-own credential and model overrides, enforced at the LLM client instantiation boundary by evaluating `context.Context`
- `provider-model-selection`: Model catalog management — fetch available embedding and generative models from provider APIs after credential setup; persist supported model lists to database cache; allow org/project to select active embedding model and default generative model
- `llm-cost-tracking`: Record token/media usage per LLM call; maintain multi-modal pricing tables per model; compute estimated cost per project/period; expose usage summary via API and CLI
- `provider-cli`: CLI commands — `emergent provider set-key` (Google AI), `emergent provider set-vertex` (Vertex AI), `emergent provider models`, `emergent provider usage`; project-level policy via `emergent projects set-provider`

### Modified Capabilities

- `configuration-management`: Existing env-var credentials (`GOOGLE_API_KEY`, `VERTEX_PROJECT`, etc.) become the lowest-priority fallback. A new `LLM_ENCRYPTION_KEY` is added to secure DB credentials.

## Impact

- **New DB tables**: `organization_provider_credentials` (org_id, provider, encrypted_credential, gcp_project, location), `organization_provider_model_selections` (org_id, provider, embedding_model, generative_model), `project_provider_policies` (project_id, provider, policy, encrypted_credential, gcp_project, location, embedding_model, generative_model), `provider_supported_models` (provider, model_name, type, last_synced), `llm_usage_events` (project_id, provider, model, text_input_tokens, image_input_tokens, video_input_tokens, audio_input_tokens, output_tokens, cost_usd, timestamp), `provider_pricing` (provider, model, text_input_price, image_price, video_price, audio_price, output_price)
- **External API calls**: Google AI API (`/v1beta/models`) and Vertex AI API (`/v1/projects/{project}/locations/{location}/models`) to enumerate models.
- **Server-go domains**: `ModelFactory` and `genai.Client` refactored from singletons to resolve credentials per-request via injected context; usage tracking via custom `model.LLM` wrapper.
- **API/SDK/CLI**: New Go SDK client methods; new `emergent provider` subcommand group in `tools/emergent-cli`.