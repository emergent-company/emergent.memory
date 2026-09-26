## Why

Model selection in Emergent Memory is a free-form string that appears in two inconsistent shapes: sometimes bare (`gpt-4o`), sometimes provider-prefixed (`openai/gpt-4o`). The prefix names a **provider type** — one of four closed values (`google`, `google-vertex`, `openai`, `deepseek`) — and provider config rows are unique per `(project_id, provider)`. Two problems follow.

1. **Ambiguous, inconsistently-parsed references.** Provider configs store the bare model (`domain/provider/service.go` `stripModelPrefix`), while routing rebuilds `type/model` and at least seven call sites re-parse the string with a "must contain exactly one `/`" heuristic to protect Vertex resource paths (`pkg/adk/model.go`, `domain/modelconfig/adapter.go`, `domain/provider/service.go`). Nothing guarantees a reference is well-formed, and the same model can be written several ways (`gpt-4o`, `openai/gpt-4o`, a full proxy id).
2. **One instance per provider type.** Because identity *is* the type, a project cannot configure two OpenAI-compatible endpoints (e.g. an Azure OpenAI deployment and a local LiteLLM proxy) even though a `base_url` column already exists. Catalog, pricing, and usage tables are keyed by type and therefore cannot distinguish instances.

This change separates **dialect** (wire protocol + auth behavior) from **provider instance** (a project-scoped, user-named config row) and makes the model reference a structured `{provider, model}` value across storage, APIs, agent definitions, and run records.

## What Changes

- Introduce `ProviderDialect` (rename of `ProviderType`, with a compatibility type alias) and a project-scoped, user-named `ProviderSlug` instance identity.
- Allow multiple provider configs per project that share a dialect; uniqueness moves from `(project_id, provider)` to `(project_id, slug)`.
- Introduce a canonical structured `ModelRef { Provider ProviderSlug; Model string }`. The `"slug/model"` string form is confined to input/display edges (CLI args, URL path segments, form values) and parsed exactly once at the boundary, splitting on the first `/`.
- Resolution is slug-first with a legacy dialect fallback: an unknown first segment that names a dialect resolves to that dialect's default instance.
- Re-key instance-sensitive data by slug: project/org custom pricing and `llm_usage_events`. Retail `provider_pricing` and `provider_supported_models` stay dialect-scoped.
- Update server APIs, the SDK provider client, the CLI `provider`/`projects` commands, and the web-ui provider/model settings surfaces to speak slug + structured reference.
- Migrate `kb.project_model_config`, `kb.agent_definitions.model` (JSONB), and `kb.agent_runs` to structured references; backfill all existing rows so current references keep resolving.

## Capabilities

### New Capabilities

- `provider-instances`: project-scoped, user-named provider config rows (slug) that may share a dialect, with slug-first resolution and legacy dialect aliases.
- `model-references`: a canonical structured model reference (`{provider, model}`) used in storage, APIs, agent definitions, and run records, with the string form confined to input/display edges.

### Modified Capabilities

- `provider-settings`: provider configuration and model-default dropdowns/Test actions operate on provider instances (slug) and structured references.
- `usage-dashboard`: usage rows distinguish the provider instance (slug) from the dialect, so two instances of one dialect are not merged.
- `openai-compatible-llm`: an OpenAI-compatible endpoint becomes a dialect that a project may instantiate more than once.

## Impact

**Server (`apps/server/`)**
- `domain/provider/`: `entity.go` (dialect + slug, config/response entities), `service.go` (resolve by slug, dialect fallback, `ModelRef` construction), `repository.go` (slug lookups, usage/pricing keying), `registry.go` (dialect registry), `catalog.go`, `usage_service.go`, `tracking_model.go`, `handler.go`, `routes.go`, adapters in `adk_adapter.go` / `usage_tracker_adapter.go`.
- `domain/modelconfig/`: `entity.go`, `service.go`, `adapter.go`, `store.go`.
- `domain/agents/`: `entity.go` (`ModelConfig` gains a provider field), `executor.go` (build `ModelRef`, persist slug/dialect on the run), run/handler model fields.
- `pkg/adk/`: `model.go` (`CreateModelWithName` takes a `ModelRef`), `credentials.go`, `embeddings` resolver seam.
- `pkg/modelref/` (new): dependency-neutral `ModelRef`, strict `ParseModelRef`, and backfill-only `NormalizeLegacy` — shared by `pkg/adk` and `domain/provider` without an import cycle.
- String-based model callers to convert: `domain/agents/session_compressor.go`, `domain/agents/handler.go`, `domain/agents/mcp_tools.go`, `domain/provider/project_settings_store.go`, `domain/provider/share_service.go`, `domain/extraction/usage.go`.
- `migrations/`: new migration(s) covering provider config slug/dialect (including dropping the pre-existing unnamed `UNIQUE (…, provider)` constraints and normalizing already-prefixed model columns — recognised-prefix strip only), `project_model_config`, `agent_definitions.model`, `agent_runs`, `llm_usage_events`, and project custom pricing (backfill + uniqueness swap). `organization_custom_pricing` stays dialect-scoped.

**SDK / CLI / Web UI**
- `apps/server/pkg/sdk/provider/client.go`: slug/dialect types and slug-addressed methods.
- `apps/cli/internal/cmd/projects.go` and `provider.go`: `configure-project <dialect> [--name <slug>]`, slug-addressed get/list/delete/test, `set-models` accepting a structured reference.
- `apps/web-ui/gateway/`: `settings_providers.go`, `project_settings_templ.go`, `agent.go` (model-issue classification by slug), provider/model handlers and templ.

**Docs / specs**: this change; the affected main specs are updated on archive.

## Non-Goals

- No change to the keying of `kb.provider_supported_models` or retail `kb.provider_pricing` (dialect-scoped) — custom endpoints of one dialect intentionally share the catalog.
- No change to env-var-only LLM configuration (`DEEPSEEK_*`, `OPENAI_*`, `VERTEX_AI_*`) beyond tolerating dialect-prefixed values.
- No cross-project provider-instance sharing, and no reintroduction of org-level provider config (currently deprecated).
