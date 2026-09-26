## Context

Provider credentials and model selection live in `domain/provider`. `ProviderType` is a closed enum (`google`, `google-vertex`, `openai`, `deepseek`) that currently serves two roles: it selects how to build an LLM client (dialect) **and** it is the uniqueness key for a credential row (`UNIQUE (project_id, provider)` in `migrations/00042_refactor_provider_configs.sql`). A `base_url` override was later added (`migrations/00080_add_openai_compatible_provider.sql`) but a project still gets only one row per type.

Model references are strings. Provider configs store the bare model (stripped by `stripModelPrefix`), routing rebuilds `type/model`, and every consumer re-parses with a "exactly one `/`" heuristic so Vertex resource paths (`publishers/google/models/...`) aren't mistaken for provider prefixes. Consumers touching `ProviderType`: ~34 non-test files.

Instance-sensitive tables are keyed by dialect today: `kb.llm_usage_events(provider, model)`, `kb.project_custom_pricing(project_id, provider, model)`, `kb.organization_custom_pricing(org_id, provider, model)`. Dialect-scoped tables: `kb.provider_supported_models(provider, model_name)`, `kb.provider_pricing(provider, model)`.

## Goals / Non-Goals

**Goals**

- One canonical, structured model reference everywhere in storage and in-process code.
- Two (or more) provider instances of the same dialect, addressable distinctly.
- Existing references and configs keep working after migration with no user action.
- Deterministic, single-boundary parsing of the string form.

**Non-Goals**

- Instance-scoping the global model catalog or retail pricing (dialect-scoped by decision).
- Cross-project instance sharing.
- Org-level provider config (stays deprecated).

## Decisions

### D1 — Separate dialect from instance identity

`ProviderDialect` is the rename of `ProviderType` and keeps the existing four wire/auth behaviors. `ProviderSlug` identifies a config row and is unique per project. `ProviderType` remains as a type alias (`type ProviderType = ProviderDialect`) for a transition period so the ~34-file rename can land incrementally without a flag day; the alias is removed in a follow-up.

Rejected: a `dialect/slug/model` three-part string — dialect is a property of the instance, so repeating it in a reference is redundant and creates a second source of truth.

### D2 — Structured `ModelRef` is canonical; strings only at edges

```go
type ModelRef struct {
    Provider ProviderSlug // instance
    Model    string       // bare model; may itself contain '/' (Vertex resource paths)
}
```

`ModelRef` is what entities, services, API payloads, agent definitions, and run records carry. The string form `"slug/model"` exists for CLI arguments, URL path segments, and form values; `ParseModelRef` splits on the **first** `/` and is the only place string parsing happens. Because `Model` is allowed to contain slashes, the "exactly one slash" heuristic disappears.

### D3 — Slug-first resolution with dialect fallback

Resolving a `ModelRef` looks up the instance by slug within the project. If no such slug exists and the first segment equals a known dialect, resolution falls back to that dialect's default instance (the legacy row backfilled with `slug = dialect`). This makes every pre-migration reference (`openai/gpt-4o`) resolve unchanged. An unknown segment that is neither a slug nor a dialect is a validation error.

### D4 — Uniqueness `(project_id, slug)`, default slug = dialect

Each provider config row gains `slug` and `dialect` columns. On migration, `dialect := provider` and `slug := provider`, preserving default-instance behavior. The old `provider` column is retained as a legacy alias for one release and dropped in a later migration. A project may now hold `UNIQUE (project_id, slug)` rows across any mix of dialects.

### D5 — Pricing/usage keyed by slug; catalog/retail by dialect

- Project and org custom pricing key on the instance slug (`UNIQUE (project_id, provider_slug, model)`), since manual rates are per endpoint.
- `kb.llm_usage_events` gains `provider_slug` and `dialect`; cost resolution first tries the project override by slug, then falls back to retail by dialect, then by model name (unchanged optimistic-matching chain).
- `provider_supported_models` and `provider_pricing` keep dialect keying. Trade-off accepted: two instances of one dialect share one catalog and retail price list.

### D6 — Migration in place, additive then tightening

Migrations add nullable columns, backfill from existing values, then set `NOT NULL` and swap uniqueness.

- **Provider configs.** The existing inline `UNIQUE (project_id, provider)` (and `UNIQUE (org_id, provider)`) constraints must be **explicitly dropped** (they are unnamed, so the migration references their PostgreSQL auto-generated names) before `UNIQUE (project_id, slug)` / `UNIQUE (org_id, slug)` is added, otherwise two same-dialect rows still collide on `provider`.
- **Provider-config model columns.** `generative_model`/`embedding_model` may already hold prefix-qualified values (the code comments explicitly bless `deepseek/deepseek-v4-flash` served via a proxy; the prefix is only stripped on read). The backfill must first-slash-split and normalize these columns too, not just `slug`/`dialect`, or removing the "exactly one slash" heuristic (D2) would mis-read a stored prefixed name as a bare model containing a slash.
- **`kb.project_model_config`.** Replaced by `*_provider_slug` + `*_model` pairs. `00124` only prefixed rows where the value lacked any `/`; a bare Vertex resource path (contains `/`, no provider prefix) was skipped and would be mis-split by first-`/`. The backfill SHALL be per-row deterministic: a value whose first `/`-segment is a known dialect becomes that dialect's default instance; an explicit provider prefix that is an existing slug becomes that instance; anything else is attributed to the project's default instance for the row's recorded dialect, or flagged when ambiguous.
- **`kb.agent_definitions.model`.** JSONB `provider` slug backfilled by prefix parse. `AgentDefinition` has a `project_id` column, so the backfill **can** join `project_provider_configs` — the constraint is ambiguity when a project holds multiple same-dialect instances, not lack of project context. Bare legacy names resolve to the project's default instance; ambiguous cases are flagged in the migration output.
- **`kb.agent_runs`.** `provider_slug`/`dialect` backfilled from the nullable `provider`. Rows with `provider IS NULL` (pre-`00084`) fall back to a dialect inferred from the model name when possible, else remain NULL and are labelled "legacy" in the dashboard rather than folded into an empty bucket.

### D7 — Edge inputs remain ergonomic

CLI: `memory provider configure-project <dialect> [--name <slug>]`; get/list/delete/test accept a slug; `projects set-models --generative <slug>/<model>` (parsed once) or explicit `--generative-provider`. API: config routes address `:slug` (a dialect value is accepted as a legacy alias); the catalog route keeps addressing `:dialect`. UI form values carry the structured pair.

### D8 — Default-instance selection order for multi-instance projects

`ResolveAny`, `ResolveAnyEmbedding`, `DefaultGenerativeModel`, and `DefaultEmbeddingModel` pick a single instance when no reference is given. They SHALL iterate dialects in the existing preference order and, within a dialect, select the instance with the lexicographically smallest slug (the default instance, whose slug equals the dialect, therefore wins). This keeps pre-migration behavior stable and makes the choice deterministic. `Resolve`/`ResolveFor` keep **dialect** semantics for backward compatibility and gain slug-addressed siblings; calling the dialect form when a dialect has multiple instances selects that dialect's default instance.

### D9 — Slug validation

A slug SHALL match `[a-z0-9][a-z0-9-]*` and SHALL NOT equal a dialect name that differs from the instance's own dialect (which would shadow the legacy dialect fallback). Creating a second instance of a dialect with no explicit slug SHALL auto-suffix (`openai-2`, `openai-3`, …) rather than collide on the default slug.

## Risks / Trade-offs

- **Agent JSONB backfill** must handle both bare and `dialect/model` legacy names. `AgentDefinition.project_id` is available, so the migration can join provider configs; the real limitation is ambiguity when a project holds multiple same-dialect instances (a bare `gpt-4o` cannot be attributed to one `openai` slug deterministically). Bare names resolve to the project's default instance and ambiguous cases are surfaced in the migration notes for review.
- **Type rename breadth** (~34 files) is the largest mechanical cost; the alias keeps it incrementally shippable but leaves a temporary dual vocabulary. Reviewer input wanted on whether to rename fully in the implementation PR or keep `ProviderType` as the permanent name for the dialect.
- **Shared catalog for same-dialect instances** (D5) means a custom endpoint's model list is not isolated. Accepted for now; noted as a follow-up if it bites.
- **`provider_supported_models` model-limit lookup** (`ModelLimitAdapter`) currently resolves by model name only; with instances it should prefer the instance's dialect.

## Migration Plan

1. Add `slug`/`dialect` to `org_provider_configs` and `project_provider_configs`; backfill `slug := provider`, `dialect := provider`; **drop the old unnamed `UNIQUE (…, provider)` constraints** and add `UNIQUE (project_id, slug)` / `UNIQUE (org_id, slug)`; keep `provider`.
2. First-slash-split and normalize the existing `generative_model`/`embedding_model` values on the provider configs (they may already be prefix-qualified) so they read as bare model names under the new parser.
3. Add structured columns to `kb.project_model_config`; backfill per-row deterministically; drop the text model columns.
4. Add `provider` to `agent_definitions.model`; backfill via prefix parse (joining provider configs by `project_id`).
5. Add `provider_slug`/`dialect` to `agent_runs`; backfill, leaving genuinely unknown rows NULL and labelled "legacy".
6. Add `provider_slug`/`dialect` to `llm_usage_events`; backfill; new index.
7. Add `provider_slug` to both custom-pricing tables; backfill; swap uniqueness.
8. Ship server, SDK, CLI, and UI changes together; no feature flag (additive columns are backward compatible during rollout).
9. Follow-up migration drops the legacy `provider` columns once no reader uses them.

## Open Questions

1. Should `ProviderType` be fully renamed to `ProviderDialect` now, or kept as the permanent name with `slug` added beside it? (Affects ~34 files.)
2. Drop the legacy `provider` columns in this change or one release later?
3. For `kb.agent_definitions.model`, is a JSONB `provider` field acceptable, or should the provider slug become a first-class column for indexability?
4. Does org-level custom pricing need instance granularity at all, given org provider config is deprecated?
5. Confirm the D8 default-instance selection order (dialect preference, then smallest slug) is the intended tiebreak when a project has multiple same-dialect instances.
6. How should a project with multiple same-dialect instances disambiguate a legacy bare model name during backfill — default instance, or fail the migration for manual resolution?
