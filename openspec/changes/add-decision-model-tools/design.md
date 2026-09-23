## Context

See `proposal.md` — Why. Grounding research: `docs/research/system-one-models/JEV_LAYA.md`.

Two facts from that research drive this design:

- The decision model is **reachable today without server code changes**. Memory's external MCP server registry already supports local-process and remote transports, per-project scoping, tool sync with declared schemas, and per-tool enablement. The open-weight Laya package ships a local-process MCP server (status, routing, prediction, and preset tools according to its own documentation) and a Jev-compatible HTTP server as an alternative.
- The model is **not usable zero-shot for decisions we care about**. Published numbers: base checkpoints are near chance on typed-decision tasks; quality collapses above roughly twenty options in a single question; the English checkpoint answers non-Latin script at high confidence while being wrong; and raw probabilities are heavily over-confident until a temperature fit is applied.

The second set of facts is what turns this from a wiring exercise into a spec: the tier is only useful if its confidence semantics, option ceiling, language routing, and calibration precondition are contractual.

## Goals / Non-Goals

**Goals**

- Specify the decision tier as a behavior contract that any decision model can satisfy, not just one vendor.
- Make the failure mode of trusting uncalibrated confidence impossible by contract.
- Guarantee that adopting the tier cannot regress extraction or agent behavior for projects that do not opt in.

**Non-Goals**

- Implementing the tier (this change is spec-only).
- Provisioning the model, its weights, or its host process.
- A first-class decision-model type in the provider/model-configuration surface. Deliberately deferred; the registry route is sufficient to prove value first.
- Own training or on-device inference.
- Replacing any part of LLM content extraction.

## Decisions

**Reuse the external MCP server surface rather than adding a model type.**
The registry already provides registration, per-project scoping, transports, tool sync, schema declaration, enablement, and agent pool integration. A new model type would duplicate all of it and require migrations, provider catalog changes, and a new client. Rejected alternative: extending the provider model-type enum — deferred until the tier proves value.

**Register per project, not globally.**
The registry is project-scoped by construction, and calibration is inherently per-project data. A global registration would imply one calibration for all projects, which the calibration requirement forbids. Rejected alternative: org-wide decision model sharing — possible later, but it cannot be specified safely before calibration ownership is settled.

**Operations are a prerequisite, not part of the contract.**
The model process runs on the server host (local-process transport) or as a reachable service. Deployment, weights provisioning, and pinning the model version are operational tasks in `tasks.md` open to the operator, not requirements on Memory's behavior.

**Calibration is a recorded artifact, not a runtime heuristic.**
The contract requires a recorded fit per question type and option count, and forbids gating autonomous behavior without one. Rejected alternative: gate on raw confidence with a conservative threshold — the published over-confidence (mean expected calibration error around 0.47 before fitting and around 0.08 after for the English checkpoint, and around 0.31 before fitting and around 0.11 after for the multilingual one — the checkpoint the language-routing requirement depends on) makes any fixed raw threshold arbitrary.

**Triage sits before extraction and can only skip.**
The decision model is a gate, never a source. This preserves the existing LLM extraction path as the sole producer of graph content and keeps the blast radius of a bad decision to "an extraction batch was not extracted". Recovery is not automatic: re-extraction currently re-runs the whole document through the same batch loop, so the contract requires that a re-extraction request bypasses triage — otherwise a skip would deterministically repeat on every retry.

**Fallback is fail-open.**
On error, timeout, or malformed response, the flow proceeds exactly as it does today. Fail-closed would let an optional optimization break core ingestion; the tier must never be on the critical path.

## Risks / Trade-offs

- **Silent quality loss from triage skipping good batches** → Only calibrated high-confidence skips are honoured; every skip is recorded; a re-extraction request bypasses triage so a skip is recoverable; triage is off by default.
- **Per-project registration friction** → Accepted as the cost of per-project calibration ownership; the registration recipe is short and scriptable.
- **Model/version drift invalidating calibration** → Calibration SHALL be recorded against the model identity reported by the serving model; a version change requires re-fitting. Operators pin the exact package version.
- **Local-process model runs on the server host** → Operators must provision the runtime and weights on the host; if that is undesirable, the remote transport is the alternative and carries the same contractual behavior.
- **Over-confidence of the boolean primitive and of ordinal scoring** → Both are weaker primitives by the vendor's own reporting; the calibration requirement covers them, and use of `score` beyond ranking should be treated cautiously.
- **Spec-only change drifting before implementation** → The change is archived only after implementation and verification; the delta specs are the contract the implementation is measured against.

## Migration Plan

Additive, but not migration-free. Three artifacts the contract depends on have no storage today: the calibration record, the decision audit trail, and per-project triage configuration (enablement, threshold, timeout). Plan on one minimal migration: a calibration record keyed by model identity + question type + option count, a decision audit record, and a configuration slot. There is no existing calibration or decision-temperature concept in the codebase — the only `Temperature` usages today are LLM generation config.

1. Operator provisions the decision model host runtime and pins a model version.
2. Register the model for a project and sync tools; verify the tools appear enabled at the server level but reach no agent until a whitelist references them.
3. Collect held-out project data and perform the calibration fit; record the result.
4. Opt in one agent whitelist and observe decisions in audit records without any gating.
5. Enable triage for the project only after calibration is recorded and the threshold is agreed.

Rollback at any step: disable the server or the individual tools, or leave triage disabled; behavior returns to the pre-adoption path with no data migration to reverse.

## Open Questions

- Which model version is pinned for the first deployment, and who owns re-fitting calibration when it changes?
- What default threshold, if any, should be recommended for triage and for agent gating?
- Does the practical benefit justify promoting the tier into the provider surface (a decision model type), or is the registry route enough?
- Should a first-party hosted HTTP alternative be offered so a decision model can be used without a local runtime?
