## Why

Memory currently uses a generative LLM for decisions that need no generation — document triage, tool selection, relevance grading, guardrail checks — paying both latency and token cost for them. Small, non-autoregressive "System One" decision models (open-weight Laya; closed-API, weightless TypeSafe Jev) answer typed questions with calibrated probabilities in a single forward pass on CPU, which is a better fit for that tier. Nothing in the platform models this tier today: MCP tools, agent whitelists, and extraction triage have no notion of a decision model, its confidence semantics, or its limits. This change specifies that tier before any code is written — see `docs/research/system-one-models/JEV_LAYA.md` for the underlying research.

## What Changes

- Introduce a **decision-model tier**: a project-scoped external decision model, registered and reached through the existing external MCP server surface, exposing the model's own decision tools (status, routing, prediction, presets).
- Define the **typed-decision contract** for consumers: a state plus typed questions in, typed answers with confidence and routing metadata out, and never generated text.
- Specify the **question primitives** (`choice`, `score`, `noul`), including validation errors and the option-cardinality ceiling beyond which a single question must not be sent.
- Require **language-aware routing** so a non-Latin-script state is never answered by a checkpoint that cannot read it while reporting high confidence.
- Make **calibration a precondition for trust**: confidence SHALL NOT gate autonomous behavior until a temperature fit on project data has been applied and recorded.
- Make agent exposure **explicitly opt-in** per agent tool whitelist, and never part of default agent definitions.
- Specify the **extraction triage** use: an optional, off-by-default pre-extraction gate that can skip chunks, but SHALL NOT supply entity or relationship content.
- Require **fallback and observability**: an unavailable or failing decision server SHALL degrade to existing behavior rather than block extraction or agent runs, and decisions SHALL be recorded for audit.
- No breaking changes: all of the above is additive and disabled by default.

## Capabilities

### New Capabilities

- `decision-model-tools`: the decision-model tier — registration and discovery of a project-scoped decision model, the typed-decision contract and its primitives, cardinality and language-routing constraints, calibration-before-trust rules, opt-in agent exposure, the optional extraction triage gate, and failure/audit behavior.

### Modified Capabilities

- None. This change adds a new tier alongside the existing MCP tool surface, agent whitelisting, and extraction pipeline without changing their existing requirements.

## Impact

- **`apps/server/domain/mcpregistry/`**: existing external MCP server CRUD, sync, and stdio/HTTP proxy are reused unchanged; the tier is specified against that surface.
- **`apps/server/domain/mcp/`** and **`apps/server/domain/agents/`**: decision tools become addressable through the existing tool pool and agent whitelist; no default whitelist changes.
- **`apps/server/domain/extraction/`**: an optional triage stage is specified ahead of LLM extraction; the existing LLM extraction path remains authoritative for content.
- **`apps/server/domain/provider/`** and **`modelconfig`**: untouched by this change; a first-class model type for decision models is deliberately out of scope.
- **Operations**: the decision model runs as a process or service reachable by the server host; deployment, weights provisioning, and calibration data ownership are prerequisites, not part of this change.
- **Persistence**: this change is additive and opt-in, but it is **not migration-free**. The calibration record, the decision audit trail, and per-project triage configuration have no existing storage in the codebase, so implementation requires a minimal migration — a calibration record keyed by model identity, question type, and option count; a decision audit record; and a configuration slot for triage enablement, threshold, and timeout. See `design.md` — Migration Plan.
- **No breaking changes.** All specified behavior is opt-in per project; a project that registers nothing and enables nothing behaves exactly as today.
