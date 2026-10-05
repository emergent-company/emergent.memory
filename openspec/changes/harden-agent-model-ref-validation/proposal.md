# harden-agent-model-ref-validation

## Why

#1448 tightened the REST agent-definition write path (`CreateDefinition` /
`UpdateDefinition`) to validate `model.name` with `modelref.Parse`, but three
gaps remained:

- **#1465** Two other write paths still set `Model.Name` raw and bypass the
  invariant: the MCP `create`/`update_agent_definition` tools
  (`domain/agents/mcp_tools.go`) and the project agent-override write path
  (`SetAgentOverride` → `ApplyAgentOverride`, `domain/agents/project_settings_store.go`).
  A malformed value such as `/deepseek-v4-flash` is persisted verbatim and only
  fails later at execution.
- **#1466** `modelref.Parse` splits on the first `/` and accepts a model segment
  that begins with `/`, so `openai//gpt-4o` parses to
  `{Provider: "openai", Model: "/gpt-4o"}` and round-trips. The strict boundary
  #1448 tightened still lets this malformed reference through.
- **#1467** `agents.ModelConfig` carries a structured `Provider` slug, but the
  REST write path requires `model.name` to carry a provider prefix and reads no
  structured provider, so `{"name":"gpt-4o","provider":"openai"}` is rejected and
  the executor's provider-merge branch never fires for REST-written definitions.

Two competing representations of a model reference (name-prefix vs. separate
`Provider` slug) were silently inconsistent.

## What Changes

- `modelref.Parse` rejects a model segment that starts with `/` (doubled
  separator), covering `openai//gpt-4o`, `openai//`, `openai///x` (#1466).
- New `modelref.ParseWithProvider(name, provider)` accepts either the canonical
  `provider/model` prefix or a structured bare name + provider slug, rejects
  conflicting providers and a provider slug containing `/`, and never
  reinterprets a malformed prefixed name (#1467).
- New shared funnel `agents.NormalizeAgentModelConfig` canonicalizes an agent
  model config to the single `provider/model` form and clears `Provider`, so
  REST, MCP, and project overrides all accept and reject identically (#1465,
  #1467). Every `Model.Name`/`Provider` write path routes through it.
- The MCP `create`/`update_agent_definition` tools and `SetAgentOverride` now
  reject malformed references with the same `provider/model` error, and
  `ApplyAgentOverride` refuses to copy a malformed legacy stored value.
- Blueprint apply (`domain/blueprints/apply.go`) routes `agents[].model.name`
  through the same funnel before persisting an agent definition, closing the
  remaining verbatim-copy path.
- The MCP tool schemas expose the optional `provider` field.

## Capabilities

### Modified Capabilities

- `agent-definitions`: the model-reference validation requirement now spans all
  write paths (REST, MCP, project overrides), accepts the structured
  `provider` + bare name form, and rejects a model segment beginning with `/`.

## Impact

- `apps/server/pkg/modelref/modelref.go` — leading-`/` rejection,
  `ParseWithProvider`; `modelref_test.go` cases.
- `apps/server/domain/agents/model_ref.go` — new shared
  `NormalizeAgentModelConfig` + `ErrInvalidAgentModelRef`.
- `apps/server/domain/agents/handler.go` — REST create/update and
  `SetAgentOverride` use the funnel.
- `apps/server/domain/agents/mcp_tools.go` — MCP create/update validate and fold
  structured provider; tool schemas gain `provider`.
- `apps/server/domain/agents/project_settings_store.go` — `ApplyAgentOverride`
  defensively normalizes.
- `apps/server/domain/blueprints/apply.go` — blueprint build/update paths route
  `model.name` through the funnel.
- Tests: `pkg/modelref/modelref_test.go`,
  `domain/agents/model_ref_test.go`, `model_write_paths_test.go`,
  `model_validation_test.go`, `model_normalization_db_test.go`.
- No migration, no schema change.
