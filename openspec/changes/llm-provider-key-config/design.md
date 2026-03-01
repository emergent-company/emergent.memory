## Context

Currently, LLM provider credentials (e.g., Google AI API keys, Vertex AI service accounts) are configured globally via environment variables on the server. This prevents multi-tenant isolation, meaning all organizations and projects share the same underlying credentials and billing. As the system scales, each organization must be able to bring its own credentials, define its preferred models, and override these settings at the project level. 

Additionally, Google AI and Vertex AI have different authentication mechanisms (API Key vs. Service Account JSON) and expose different model catalogs, requiring them to be treated as first-class distinct providers in the system. Usage needs to be tracked per project to enable cost monitoring and billing.

The current implementation treats `adk.ModelFactory` and `genai.Client` as singletons instantiated at application startup. To support multi-tenant policies, these must be refactored to dynamically resolve credentials based on the execution context.

## Goals / Non-Goals

**Goals:**
- Provide secure, encrypted storage for organization-level and project-level LLM credentials.
- Differentiate and properly support both Google AI (API Key) and Vertex AI (Service Account, Project ID, Region).
- Implement a hierarchical credential resolution system (Project -> Organization -> Server Environment).
- Fetch, cache, and allow selection of supported embedding and generative models per provider.
- Track token usage (accounting for multi-modal inputs) and compute estimated costs per LLM call.
- Expose SDK methods and CLI commands (`emergent provider`) to manage policies and view usage.
- Refactor dependency injection to support per-request context evaluation for credentials.

**Non-Goals:**
- Implementing actual billing or invoicing systems.
- Supporting providers other than Google AI and Vertex AI in this specific iteration.
- Modifying the core embedding/RAG logic beyond how it instantiates the LLM clients.

## Decisions

### 1. Database Schema for Credentials & Policies
We will introduce six new tables to handle hierarchical configuration:
- `organization_provider_credentials`: Stores encrypted credentials, provider type, and Vertex metadata.
- `organization_provider_model_selections`: Stores the user-selected default embedding and generative models.
- `project_provider_policies`: Links a project to a policy (`none`, `organization`, `project`). Includes columns for overridden credentials, gcp_project, location, and models.
- `provider_supported_models`: A cache of the supported model catalog fetched from external APIs.
- `provider_pricing`: A global table defining multi-modal retail pricing (text input, image input, video input, audio input, text output) per model. Includes `last_synced` (timestamp).
- `organization_custom_pricing`: A tenant-specific override table. If an organization has negotiated custom GCP rates, their specific multi-modal pricing is stored here per model.

*Rationale:* Normalizing model selections away from credentials allows users to update preferred models without re-authenticating. Storing a cache of supported models ensures we don't query external APIs on every request. Scoping custom pricing overrides to the organization ensures accurate cost tracking in multi-tenant environments where users bring their own credentials and enterprise discounts.

### 2. Encryption of Credentials
Credentials will be encrypted at rest using a symmetric encryption key (AES-GCM) provided by a new environment variable: `LLM_ENCRYPTION_KEY`.

*Rationale:* Storing plaintext API keys or service accounts is a critical security vulnerability. 

### 3. Context Propagation & Dependency Injection Refactor
`auth.Middleware` will inject the `ProjectID` and `OrgID` into the `echo.Context`, which must be mapped into the standard `context.Context` before being passed down to service layers. `ModelFactory` and `genai.Client` factory functions will be refactored to evaluate this `context.Context` on every `CreateModel` or `EmbedDocuments` call to resolve the correct hierarchy (Project Override -> Org -> Global fallback).

*Rationale:* This is required because singletons initialized at application startup cannot serve multi-tenant boundaries.

### 4. Provider Model Synchronization
When a credential is saved, the server makes an API call to the provider to fetch the list of supported models and saves it to `provider_supported_models`. If this API call fails (e.g., timeout, but credentials are valid), the system will fall back to inserting a statically defined known list of models so the user isn't blocked.

*Rationale:* Ensuring the user can select a model even if the provider's discovery API is temporarily unavailable provides a better UX.

### 5. Usage Tracking & Dynamic Pricing Sync
To track token usage, we will create a custom struct that wraps the `google.golang.org/adk/model.LLM` interface. 

**Structure:**
- `TrackingModel`: A struct containing the underlying `model.LLM`, the `ProjectID`, and a reference to the `UsageService`.
- **Interception**: It will override the `GenerateContent` method.
- **Extraction**: Extract `UsageMetadata` from the ADK/GenAI response, mapping the fields to specific modalities (text, image, video).
- **Cost Calculation (Estimated)**: Usage events will be computed by resolving the price via hierarchy: **`organization_custom_pricing` -> `provider_pricing`**. The calculated event is dispatched asynchronously. The UI/CLI will explicitly present this as an **"Estimated Cost"** and display the math (`[Tokens used] × [Price per token]`).
- **Dynamic Pricing Sync**: A daily background cron job will fetch the latest retail pricing from an external registry (e.g., `models.dev` or a dedicated GitHub JSON file). It will update the global `provider_pricing` table and set `last_synced`. Since custom enterprise rates are isolated in `organization_custom_pricing`, the global sync safely updates retail rates without destroying tenant-specific discounts.

*Rationale:* Dynamic syncing prevents "stale pricing" without requiring a server update every time Google drops prices. Allowing a custom override ensures enterprise deployments with negotiated discounts can still track costs accurately. Wrapping the ADK interface is the cleanest way to guarantee all agent operations are tracked.

## Risks / Trade-offs

- **[Risk] Syncing models during credential save adds latency.** → *Mitigation:* API calls will have a strict timeout and fallback to static lists if they fail but credentials are valid.
- **[Risk] Asynchronous usage tracking could lose events on crash.** → *Mitigation:* For MVP, in-memory buffered channels with graceful shutdown flushing are sufficient for estimated costs.
- **[Risk] Missing project context in deep layers.** → *Mitigation:* Ensure `auth.Middleware` or request handlers strictly propagate the enriched `context.Context`.

## Migration Plan

1. **Schema Migration:** Create the new tables.
2. **Infrastructure:** Add `LLM_ENCRYPTION_KEY` to Infisical/config.
3. **Application Rollout:** Deploy the new provider domain logic, refactor `ModelFactory` context resolution, and expose the new APIs.
4. **SDK & CLI Update:** Add endpoints to the Go SDK and release the updated CLI with `provider` subcommands.