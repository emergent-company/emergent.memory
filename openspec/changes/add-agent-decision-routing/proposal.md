## Why

We run AI coding agents via Paseo (orchestration) and OpenCode (agent harness). Today every agent lane uses a fixed model, and there is no pre-LLM guardrail and no per-task model routing: a task goes straight to whatever model the lane is pinned to, regardless of whether it is a one-line typo or a risky cross-service migration. Research into "System One" decision models — non-generative models that produce typed answers with calibrated confidence instead of generating tokens — shows there is a cheap, deterministic layer we can put *before* the LLM to classify each task into a lane/model/variant and to reject obviously dangerous directives before any model tokens are spent.

## What Changes

- A new cross-cutting **decision layer** that, for each incoming agent task, (1) runs a guardrail gate against high-confidence dangerous-directive patterns, and (2) deterministically classifies the task text into a lane, model, and reasoning variant using an ordered, data-driven routing table (first match wins, documented default fallback).
- An **optional local decision backend** (Laya, `github.com/NandhaKishorM/laya`, Apache-2.0): when a backend endpoint is configured and reachable, classification/guardrail decisions may be delegated to it, with the result recording which backend produced the decision. Any backend error, timeout, or malformed response degrades gracefully to the deterministic heuristics.
- A **stdlib-only Python 3 CLI** as the implementation surface (`tools/agent-routing/route.py`) with a routable contract: task text via stdin/arg, `--json` output, exit code `0` = allow, `3` = guardrail block, `2` = usage error. No model dependency, no network required for the authoritative path.
- **Workflow wiring** so the decision layer is actually consulted per task: an OpenCode command (`route-task`) and an `AGENTS.md` policy section that documents the routing table, guardrail patterns, and escalation policy.

## Capabilities

### New Capabilities

- `agent-decision-routing`: a cross-cutting decision layer that classifies an agent task into a lane/model/variant, gates dangerous directives before any LLM call, optionally delegates to a local Laya backend when available, degrades gracefully to deterministic heuristics, and emits machine-readable JSON.

### Modified Capabilities

None.

## Impact

- New tool `tools/agent-routing/route.py` (stdlib-only Python 3) and its deterministic unit tests `tools/agent-routing/test_route.py` (no network, no new dependencies).
- New workflow surface `.opencode/command/route-task.md` and a policy section in `AGENTS.md` documenting the routing table, guardrail patterns, and escalation policy.
- Optional local backend: the tool reads an endpoint from environment/configuration; when absent or unreachable the deterministic path is authoritative. No change to existing Paseo/OpenCode model configuration is required.
- No change to existing agent lanes, models, or server code. No breaking changes.
