## 1. Domain types and parser

- [ ] 1.1 Add `ProviderDialect` (rename of `ProviderType`, retained as a type alias) and `ProviderSlug` to `apps/server/domain/provider/entity.go`. Verify `go build ./...` from `apps/server/`.
- [ ] 1.2 Add `ModelRef { Provider ProviderSlug; Model string }` and `ParseModelRef(string) (ModelRef, error)` (split on first `/`) plus `String()` to a new `apps/server/domain/provider/modelref.go`. Verify `go build ./...`.
- [ ] 1.3 Unit-test `ParseModelRef`: structured pair, `slug/model`, model containing slashes (Vertex resource path), missing provider, empty model, surrounding whitespace. Verify `go test ./domain/provider/ -run ModelRef -count=1`.

## 2. Provider instances (DB + repository)

- [ ] 2.1 Migration: add `slug`/`dialect` to `kb.org_provider_configs` and `kb.project_provider_configs`, backfill `slug := provider`, `dialect := provider`, **drop the pre-existing unnamed `UNIQUE (…, provider)` constraints**, add `UNIQUE (project_id, slug)` / `UNIQUE (org_id, slug)`, and keep the legacy `provider` column. Verify `task migrate:up` then `task migrate:status`.
- [ ] 2.2 Migration: first-slash-split and normalize the existing `generative_model`/`embedding_model` values on both config tables (they may already be prefix-qualified) so they read as bare model names under the new parser. Verify `task migrate:up`.
- [ ] 2.3 Migration test: up/down round-trip and backfill assertions for default-slug rows, prefixed model columns, and the dropped-constraint case (two same-dialect rows now insertable). Verify `task test:integration`.
- [ ] 2.4 Update `ProjectProviderConfig`/`OrgProviderConfig` entities and `ProviderConfigResponse`/`ProjectProviderConfigResponse` with `Slug`/`Dialect`. Verify `go build ./...`.
- [ ] 2.5 Add `GetProjectProviderConfigBySlug`, `ListProjectProviderConfigs` (all instances), and `DeleteProjectProviderConfigBySlug` to `repository.go`; keep legacy-by-dialect helpers during transition. Verify `go build ./...`.
- [ ] 2.6 Unit-test repository slug lookups and multi-instance listing. Verify `go test ./domain/provider/ -run ProviderConfig -count=1`.
- [ ] 2.7 Implement slug validation (`[a-z0-9][a-z0-9-]*`, not shadowing a different dialect) and auto-suffix (`openai-2`, …) when a second instance of a dialect is created without an explicit slug (D9). Verify `go build ./...`.
- [ ] 2.8 Unit-test slug validation and auto-suffix on a second no-slug instance. Verify `go test ./domain/provider/ -run Slug -count=1`.

## 3. Credential resolution

- [ ] 3.1 Add `Slug`/`Dialect` to `ResolvedCredential`; keep `Provider` = dialect. Verify `go build ./...`.
- [ ] 3.2 Implement `ResolveByRef(ctx, ModelRef)` with slug-first / dialect-fallback semantics; reimplement `ResolveAny`, `ResolveAnyEmbedding`, `DefaultGenerativeModel`, `DefaultEmbeddingModel` over instances **using the D8 selection order (dialect preference, then smallest slug per dialect)** and return `ModelRef`; keep `Resolve`/`ResolveFor` dialect semantics for compatibility and add slug-addressed siblings. Verify `go build ./...`.
- [ ] 3.3 Unit-test resolution: exact slug; legacy dialect prefix → default instance; two same-dialect instances resolve to distinct credentials; multi-instance `ResolveAny` picks the deterministic default; unknown segment errors. Verify `go test ./domain/provider/ -run Resolve -count=1`.

## 4. ADK model factory

- [ ] 4.1 Change `ModelResolver.ResolveGenerativeModelByID` to return `ModelRef`; update `pkg/adk/model.go` `CreateModel`/`CreateModelWithName` to accept a `ModelRef` and build the client by `ModelRef.Provider`'s dialect. Verify `go build ./...` from `apps/server/`.
- [ ] 4.2 Keep a string shim for the env-var/test path that parses via `ParseModelRef`. Verify `go build ./...`.
- [ ] 4.3 Update `CredentialResolver.ResolveFor` and `ModelWrapper.WrapModel` signatures to carry slug + dialect; update `adk_adapter.go` and `usage_tracker_adapter.go`. Verify `go build ./...`.
- [ ] 4.4 Unit-test factory construction per dialect from a `ModelRef`, including the legacy-string shim. Verify `go test ./pkg/adk/ -count=1`.
- [ ] 4.5 Convert the remaining string-based `CreateModelWithName`/`ResolveGenerativeModelByID` callers to `ModelRef`: `domain/agents/session_compressor.go`, `domain/agents/handler.go` (model validation + `ResolveGenerativeModelByID` string return), `domain/agents/mcp_tools.go`, `domain/provider/project_settings_store.go`, `domain/provider/share_service.go`, and `domain/extraction/usage.go` (manual provider-string→enum map). Verify `go build ./...`.
- [ ] 4.6 Update `ModelLimitAdapter`/`GetModelInputLimit`/`GetModelOutputLimit` to resolve limits by the reference's dialect/instance rather than a bare model name, so the correct context window is selected across same-dialect instances. Verify `go build ./...` and `go test ./domain/provider/ -run Limit -count=1`.

## 5. Usage tracking and pricing

- [ ] 5.1 Add `provider_slug`/`dialect` to `kb.llm_usage_events` (migration), backfill, add index. Verify `task migrate:up`.
- [ ] 5.2 Add `provider_slug` to `kb.project_custom_pricing` and `kb.organization_custom_pricing`; backfill; swap uniqueness to include `provider_slug`. Verify `task migrate:up`.
- [ ] 5.3 Carry slug + dialect through `TrackingModel` and `LLMUsageEvent`; update `UsageService.calculateCostWith` so custom pricing resolves by slug and retail by dialect. Verify `go build ./...`.
- [ ] 5.4 Unit-test cost resolution precedence: project override by slug → retail by dialect → retail by model name; two same-dialect instances get distinct override hits. Verify `go test ./domain/provider/ -run Cost -count=1`.
- [ ] 5.5 Update usage summary/timeseries queries and row types to return `provider_slug` + `dialect`. Verify `go build ./...`.

## 6. Model config and agents

- [ ] 6.1 Migration: replace `kb.project_model_config` text model columns with `generative_provider_slug`+`generative_model`, `embedding_provider_slug`+`embedding_model`; backfill by first-slash split. Verify `task migrate:up`.
- [ ] 6.2 Update `domain/modelconfig` entity/service/store/adapter to store and resolve `ModelRef`. Verify `go build ./...` and `go test ./domain/modelconfig/ -count=1`.
- [ ] 6.3 Migration: add `provider` slug to `kb.agent_definitions.model` JSONB; backfill via prefix parse. Verify `task migrate:up`.
- [ ] 6.4 Add `Provider` to `agents.ModelConfig`; update `executor.go` to build a `ModelRef` from the agent override and persist `provider_slug`/`dialect` on the run. Verify `go build ./...`.
- [ ] 6.5 Migration: add `provider_slug`/`dialect` to `kb.agent_runs`; backfill from the nullable `provider`, inferring dialect from the model name where possible and leaving genuinely unknown rows NULL (labelled "legacy") rather than folding them into an empty bucket. Verify `task migrate:up`.
- [ ] 6.6 Unit-test agent model override → `ModelRef` and run-record persistence, including a legacy row with NULL provider. Verify `go test ./domain/agents/ -run Model -count=1`.

## 7. API surface

- [ ] 7.1 Change provider config routes to address `:slug` (accept a dialect as a legacy alias); update `routes.go` comments and `handler.go` param handling. Verify `go build ./...`.
- [ ] 7.2 Change pricing-override routes to `:slug`; update request/response types with `provider`/`dialect`. Verify `go build ./...`.
- [ ] 7.3 Include `provider`/`dialect` in model-catalog and usage responses. Verify `go build ./...`.
- [ ] 7.4 Handler tests: create two same-dialect instances; get/list/delete by slug; legacy dialect alias still resolves. Verify `go test ./domain/provider/ -run Handler -count=1`.

## 8. SDK, CLI, Web UI

- [ ] 8.1 Update `apps/server/pkg/sdk/provider/client.go`: add `Slug`/`Dialect` fields, slug-addressed methods, `UpsertProjectConfig(projectID, slug, req)` with `req.Dialect`. Verify `go build ./...` from `apps/server/pkg/sdk`.
- [ ] 8.2 Update CLI `provider configure-project <dialect> [--name <slug>]`, slug-addressed get/list/delete/test, and `projects set-models` structured input. Verify `go build ./...` from `apps/cli`.
- [ ] 8.3 CLI unit tests for mixed inputs (`openai-main/gpt-4o`, legacy `openai/gpt-4o`). Verify `go test ./internal/cmd/ -run Provider -count=1`.
- [ ] 8.4 Update web-ui gateway provider settings, project settings, and agent model-issue classification to instance slugs + structured references; regenerate templ. Verify `templ generate` then `go build ./...` from `apps/web-ui/gateway`.
- [ ] 8.5 Gateway unit tests: provider instance CRUD, structured dropdown values, two same-dialect instances displayed distinctly. Verify `go test ./...` from `apps/web-ui/gateway`.

## 9. Edge parsing and removal

- [ ] 9.1 Confirm the ad-hoc provider-prefix parsers are removed: `stripModelPrefix`/`stripRoutingPrefix` and the `strings.Cut`/`Count(…, "/") == 1`/`SplitN` provider-prefix sites in `domain/provider`, `domain/modelconfig`, and `pkg/adk` are deleted or delegate to `ParseModelRef`. Verify by inspecting those specific symbols (`rg 'stripModelPrefix|stripRoutingPrefix' apps/server`) rather than a generic slash-count search.
- [ ] 9.2 (Follow-up, optional) Drop legacy `provider` columns and the `ProviderType` alias once no reader remains.

## 10. Verification

- [ ] 10.1 `go build ./...` from `apps/server/`, `apps/server/pkg/sdk`, `apps/cli`, and `apps/web-ui/gateway`; all pass.
- [ ] 10.2 `task lint` at repo root and `task lint` in `apps/web-ui`; no new issues.
- [ ] 10.3 Full unit/integration suite: `task test` and `task test:integration`; all pass.
- [ ] 10.4 Migration round-trip: `task migrate:up` → `task migrate:down` → `task migrate:up` with backfill assertions.
- [ ] 10.5 End-to-end: configure two OpenAI-compatible instances, set separate defaults, run an agent against each, and confirm distinct usage rows and pricing overrides.
- [ ] 10.6 Manual UI: provider settings page shows both instances and the model dropdowns submit structured references.
- [ ] 10.7 Update the affected main specs on archive (`provider-settings`, `usage-dashboard`, `openai-compatible-llm`).
