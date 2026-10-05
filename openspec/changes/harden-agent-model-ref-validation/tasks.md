## 1. Parser (#1466)

- [x] 1.1 Reject a model segment that begins with `/` in `modelref.Parse` (`openai//gpt-4o`, `openai//`, `openai///x`).
- [x] 1.2 Add `modelref.ParseWithProvider(name, provider)`: canonical prefix or structured bare name + slug; reject conflicting providers, a slug containing `/`, and a malformed prefixed name rescued by a provider.
- [x] 1.3 Unit-test `Parse` and `ParseWithProvider` (mutation-checked: disabling the leading-`/` guard or the structured-provider path fails the tests).

## 2. Shared write-path funnel (#1465, #1467)

- [x] 2.1 Add `agents.NormalizeAgentModelConfig` + `ErrInvalidAgentModelRef` (`domain/agents/model_ref.go`) canonicalizing to `provider/model` and clearing `Provider`.
- [x] 2.2 Route `CreateDefinition` / `UpdateDefinition` through the funnel (replacing raw `modelref.Parse`).
- [x] 2.3 Route the MCP `create`/`update_agent_definition` tools through the funnel before any repository access; add `provider` to both tool schemas.
- [x] 2.4 Route `SetAgentOverride` through the funnel; make `ApplyAgentOverride` refuse to copy a malformed legacy value.
- [x] 2.6 Route blueprint apply (`domain/blueprints/apply.go`, create + update paths) through the funnel; reject a malformed manifest `model.name` before persist.
- [x] 2.5 Tests: `model_ref_test.go`, `model_write_paths_test.go`, extra `model_validation_test.go` cases, DB normalization coverage in `model_normalization_db_test.go` (REST + MCP structured provider). All mutation-checked.

## 3. Spec

- [x] 3.1 Add the `agent-definitions` MODIFIED requirement delta.

## 4. Verification

- [x] 4.1 `cd apps/server && go build ./...`.
- [x] 4.2 `go test -count=1 ./pkg/modelref/... ./domain/agents/...`.
- [x] 4.3 `bash apps/server/scripts/lint-ratchet.sh`.
- [x] 4.4 `openspec validate harden-agent-model-ref-validation --strict`.
