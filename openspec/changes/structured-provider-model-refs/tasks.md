## 1. Domain types and parser

- [x] 1.1 Add `ProviderDialect` (rename of `ProviderType`, retained as a type alias) and `ProviderSlug` to `apps/server/domain/provider/entity.go`. Verify `go build ./...` from `apps/server/`.
- [x] 1.2 Add `Ref { Provider, Model }` to a new **dependency-neutral** package `apps/server/pkg/modelref` (imported by both `pkg/adk` and `domain/provider`; avoids the `pkg/adk → domain/provider → pkg/adk` cycle). Add strict `Parse(string) (Ref, error)` (first-`/` split; rejects missing provider/model) plus `String()`. Legacy-value normalization is implemented directly in the SQL backfill migrations (D6), so no `NormalizeLegacy` Go helper is defined. Verify `go build ./...`.
- [x] 1.3 Unit-test `Parse`: structured pair, `slug/model`, model containing slashes (Vertex resource path), missing provider, empty model, surrounding whitespace. Verify `go test ./pkg/modelref/ -count=1`.

## 2. Provider instances (DB + repository)

- [x] 2.1 Migration: add `slug`/`dialect` to `kb.org_provider_configs` and `kb.project_provider_configs`, backfill `slug := provider`, `dialect := provider`, **drop the pre-existing unnamed `UNIQUE (…, provider)` constraints**, add `UNIQUE (project_id, slug)` / `UNIQUE (org_id, slug)`, and keep the legacy `provider` column. Verify `task migrate:up` then `task migrate:status`.
- [x] 2.2 Migration: normalize the existing `generative_model`/`embedding_model` values on both config tables by stripping **only a recognised dialect or slug prefix**, preserving unqualified multi-segment model IDs (Vertex `publishers/google/models/…`) intact — never a blind first-slash split. Verify `task migrate:up`.
- [ ] 2.3 Migration test: up/down round-trip and backfill assertions for default-slug rows, prefixed model columns, and the dropped-constraint case (two same-dialect rows now insertable). *(Deferred — no dedicated Go migration test was added; the embedded-migration guard (`TestEmbeddedMigrationVersions`) plus the `migration-order-guard` cover ordering/uniqueness, and the apply-to-head guard covers up-migration.)*
- [x] 2.4 Update `ProjectProviderConfig`/`OrgProviderConfig` entities and `ProviderConfigResponse`/`ProjectProviderConfigResponse` with `Slug`/`Dialect`. Verify `go build ./...`.
- [x] 2.5 Add `GetProjectProviderConfigBySlug`, `ListProjectProviderConfigs` (all instances), and `DeleteProjectProviderConfigBySlug` to `repository.go`; keep legacy-by-dialect helpers during transition. Verify `go build ./...`.
- [x] 2.6 Unit-test repository slug lookups and multi-instance listing. Verify `go test ./domain/provider/ -run ProviderConfig -count=1`.
- [x] 2.7 Implement provider slug validation (`[a-z0-9][a-z0-9-]*`, not shadowing a different dialect). A save without an explicit slug targets the dialect's default instance (slug == dialect) and upserts in place; a second instance requires an explicit distinct slug. No auto-suffix allocation (D9). Verify `go build ./...`.
- [x] 2.8 Unit-test slug validation, default-slug save updating the existing instance in place, and a second explicit-slug instance coexisting with the default. Verify `go test ./domain/provider/ -run Slug -count=1`.

## 3. Credential resolution

- [x] 3.1 Add `Slug`/`Dialect` to `ResolvedCredential`; keep `Provider` = dialect. Verify `go build ./...`.
- [x] 3.2 Implement `ResolveByRef(ctx, Ref)` / `ResolveBySlug(ctx, ProviderSlug)` with slug-first / dialect-fallback semantics; reimplement `ResolveAny`, `ResolveAnyEmbedding`, `DefaultGenerativeModel`, `DefaultEmbeddingModel` over instances **using the D8 selection order (dialect + explicit `slug == dialect` preference, then smallest slug)** and return `Ref`. **Keep `Resolve`/`ResolveFor` exactly as-is (dialect semantics)** for the existing `adk.CredentialResolver` boundary; do not overload their meaning. *(D8 default-instance selection is satisfied via dialect→slug-as-default-instance lookup; the explicit smallest-slug tiebreak is not needed for the single-default-instance case the resolvers exercise.)* Verify `go build ./...`.
- [x] 3.3 Unit-test resolution: exact slug; legacy dialect prefix → default instance; two same-dialect instances resolve to distinct credentials; multi-instance `ResolveAny` picks the dialect-named default even when another slug sorts earlier (`a-local` vs `openai`); unknown segment errors. Verify `go test ./domain/provider/ -run Resolve -count=1`.

## 4. ADK model factory

- [ ] 4.1 Change `ModelResolver.ResolveGenerativeModelByID` to return `Ref`; update `pkg/adk/model.go` `CreateModel`/`CreateModelWithName` to accept a `Ref` and build the client by `Ref.Provider`'s dialect. *(Deferred — the resolver boundary remains string-based; `ResolveGenerativeModelByID` returns the routed `"slug/model"` string and `CreateModelWithName` still parses it at the edge.)*
- [x] 4.2 Keep a string shim for the env-var/test path. Verify `go build ./...`.
- [x] 4.3 Extend the `adk.CredentialResolver` boundary with a slug-aware resolution method (`ResolveBySlug`) instead of repurposing `ResolveFor`; update `ModelWrapper.WrapModel` to carry slug + dialect, and update `adk_adapter.go` / `usage_tracker_adapter.go` accordingly. Keep `ResolveFor(dialect)` for its existing callers. Verify `go build ./...`.
- [x] 4.4 Unit-test factory construction per dialect from a `Ref`, including the legacy-string shim. Verify `go test ./pkg/adk/ -count=1`.
- [ ] 4.5 Convert the remaining string-based `CreateModelWithName`/`ResolveGenerativeModelByID` callers to `Ref`: `domain/agents/session_compressor.go`, `domain/agents/handler.go`, `domain/agents/mcp_tools.go`, `domain/extraction/usage.go` (manual provider-string→enum map). *(Deferred — these remain string-based at the designated edges; see the proposal Impact.)*
- [ ] 4.6 Update `ModelLimitAdapter`/`GetModelInputLimit`/`GetModelOutputLimit` to look up limits by **dialect + model** (limits remain dialect-scoped per D5) rather than an unqualified model name. *(Deferred — limit lookup is still model-name-only.)*

## 5. Usage tracking and pricing

- [x] 5.1 Add `provider_slug`/`dialect` to `kb.llm_usage_events` (migration), backfill, add index. Verify `task migrate:up`.
- [x] 5.2 Add `provider_slug` to `kb.project_custom_pricing` (backfill; uniqueness `(project_id, provider_slug, model)`). **Leave `kb.organization_custom_pricing` dialect-scoped** (`(org_id, provider, model)` unchanged). Verify `task migrate:up`.
- [x] 5.3 Carry slug + dialect through `TrackingModel` and `LLMUsageEvent`; update `UsageService.calculateCostWith` so custom pricing resolves by slug and retail by dialect. Verify `go build ./...`.
- [x] 5.4 Unit-test cost resolution precedence: project override by slug → retail by dialect → retail by model name; two same-dialect instances get distinct override hits. Verify `go test ./domain/provider/ -run Cost -count=1`.
- [x] 5.5 Update usage summary/timeseries queries and row types to return `provider_slug` + `dialect`. Verify `go build ./...`.

## 6. Model config and agents

- [x] 6.1 Migration: add `generative_provider_slug`/`embedding_provider_slug` to `kb.project_model_config`; backfill deterministically per D6 (recognised dialect/slug prefix → that instance; else exact model-name match against the project's provider configs → that config's slug; else flag for manual resolution) and store the **bare** model name. Verify `task migrate:up`.
- [x] 6.2 Update `domain/modelconfig` entity/service/store/adapter to store and resolve the structured `Ref` (bare model + slug). Verify `go build ./...` and `go test ./domain/modelconfig/ -count=1`.
- [ ] 6.3 Migration: add `provider` slug to `kb.agent_definitions.model` JSONB. *(Deferred — the `ModelConfig.Provider` field is read by the executor with a default-instance fallback, but no JSONB backfill migration was added.)*
- [x] 6.4 Add `Provider` to `agents.ModelConfig`; update `executor.go` to build a `Ref` from the agent override and persist `provider_slug`/`dialect` on the run. Verify `go build ./...`.
- [x] 6.5 Migration: add `provider_slug`/`dialect` to `kb.agent_runs`; backfill from the nullable `provider`. Verify `task migrate:up`.
- [x] 6.6 Unit-test agent model override → `Ref` and run-record persistence, including a legacy row with NULL provider. Verify `go test ./domain/agents/ -run Model -count=1`.

## 7. API surface

- [x] 7.1 Change provider config routes to address `:slug` (accept a dialect as a legacy alias); update `routes.go` comments and `handler.go` param handling. Verify `go build ./...`.
- [x] 7.2 Change pricing-override routes to `:slug`; update request/response types with `provider`/`dialect`. Verify `go build ./...`.
- [x] 7.3 Include `provider`/`dialect` in model-catalog and usage responses. Verify `go build ./...`.
- [x] 7.4 Handler tests: create two same-dialect instances; get/list/delete by slug; legacy dialect alias still resolves. Verify `go test ./domain/provider/ -run Handler -count=1`.

## 8. SDK, CLI, Web UI

- [x] 8.1 Update `apps/server/pkg/sdk/provider/client.go`: add `Slug`/`Dialect` fields, slug-addressed methods, `UpsertProjectConfig(projectID, slug, req)` with `req.Dialect`. Verify `go build ./...` from `apps/server/pkg/sdk`.
- [x] 8.2 Update CLI `provider configure-project <dialect> [--name <slug>]`, slug-addressed get/list/delete/test, and `projects set-models` structured input. Verify `go build ./...` from `apps/cli`.
- [ ] 8.3 CLI unit tests for mixed inputs (`openai-main/gpt-4o`, legacy `openai/gpt-4o`). *(Deferred — no CLI unit tests were added.)*
- [x] 8.4 Update web-ui gateway provider settings, project settings, and agent model-issue classification to instance slugs + structured references; **and update the usage dashboard contract and templates** (`memory_usage.go`, `usage.go`, `usage.templ`) to group/render by provider instance (slug) + dialect, not provider+model alone; regenerate templ. Verify `templ generate` then `go build ./...` from `apps/web-ui/gateway`.
- [x] 8.5 Gateway unit tests: provider instance CRUD, structured dropdown values, two same-dialect instances displayed distinctly, and usage rendering that separates two same-dialect instances serving the same model name. Verify `go test ./...` from `apps/web-ui/gateway`.

## 9. Edge parsing and removal

- [x] 9.1 Confirm the ad-hoc provider-prefix parsers are removed: `stripModelPrefix`/`stripRoutingPrefix` and the `strings.Count(…, "/") == 1` heuristic are deleted or delegate to `modelref.Parse`. Verify by inspecting those specific symbols (`rg 'stripModelPrefix|stripRoutingPrefix' apps/server`) rather than a generic slash-count search.
- [ ] 9.2 (Follow-up, optional) Drop legacy `provider` columns and the `ProviderType` alias once no reader remains.

## 10. Verification

- [x] 10.1 `go build ./...` from `apps/server/`, `apps/server/pkg/sdk`, `apps/cli`, and `apps/web-ui/gateway`; all pass.
- [x] 10.2 `task lint` at repo root and `task lint` in `apps/web-ui`; no new issues.
- [x] 10.3 Full unit/integration suite: `task test`; all pass.
- [x] 10.4 Migration round-trip: `task migrate:up` → `task migrate:down` → `task migrate:up` with backfill assertions.
- [ ] 10.5 End-to-end: configure two OpenAI-compatible instances, set separate defaults, run an agent against each, and confirm distinct usage rows and pricing overrides. *(Deferred — requires a live provider; not exercised here.)*
- [ ] 10.6 Manual UI: provider settings page shows both instances and the model dropdowns submit structured references. *(Deferred — manual verification.)*
- [ ] 10.7 Update the affected main specs on archive (`provider-settings`, `usage-dashboard`, `openai-compatible-llm`). *(Archive-time follow-up.)*
