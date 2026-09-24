## Context

We run AI coding agents via Paseo (orchestration) and OpenCode (agent harness). Each agent lane is pinned to a fixed model today, and there is no pre-LLM guardrail and no per-task routing. Research into "System One" typed-decision models surfaced two candidates:

- **Jev** (TypeSafe AI, `typesafe-ai/jev`): a non-generative "System One" typed-decision model that returns typed answers with calibrated confidence and generates no tokens. It is proprietary, and its API is **not** OpenAI-compatible (`POST /v1/systemone`, custom schema) — it cannot be used as an agent/chat model in OpenCode or Paseo, only called from code via SDK/HTTP.
- **Laya** (`github.com/NandhaKishorM/laya`, Apache-2.0): an open-source local decision model in the same problem space, self-hostable. It ships a `laya-serve` HTTP server (Jev-compatible wire protocol) and an optional MCP server; ~33ms per decision; calibrated confidence; presets for routing/guard/moderation/triage. Real consumers already exist as harness plugins (e.g. a Paseo plugin that classifies each message and picks model/effort per turn).

The conclusion this change encodes: adopt a **decision layer** for our own workflow that uses a local Laya backend when available and degrades to pure deterministic heuristics when not. Jev is not adopted as an agent model (it cannot be one); it is noted in this design as the hosted alternative to the Laya backend.

## Goals / Non-Goals

**Goals:**

- Classify each agent task into a lane, model, and reasoning variant using ordered, data-driven rules with a documented default fallback.
- Reject high-confidence dangerous-directive tasks with human-readable reasons, before any model tokens are spent, while still allowing tasks that reference those patterns to fix or guard against them.
- Optionally delegate classification/guardrail decisions to a local Laya backend when configured and reachable, recording which backend produced the decision.
- Degrade gracefully: any backend error, timeout, or malformed response falls back to deterministic heuristics, is marked degraded, and never blocks or fails the task.
- Emit machine-readable JSON for automation.

**Non-Goals:**

- No change to existing agent lanes, models, or server code.
- No adoption of Jev as an agent/chat model (impossible — non-OpenAI-compatible, no token generation); the hosted Jev is documented only as an alternative backend.
- No hosted/external decision backend in the authoritative path — the deterministic heuristics are the always-available source of truth.
- No real-time streaming or persistent storage of decisions in this change.

## Decisions

### 1. Deterministic heuristics are the always-available source of truth; the model backend is an optional accelerator

An ordered, data-driven routing table plus guardrail regexes is deterministic, dependency-free, testable, and cheap. The Laya backend (or hosted Jev) can add calibrated confidence and precision, but it is never required for a decision to be produced.

- **Alternative considered:** make the model backend the primary path with heuristics as a pure fallback. Rejected — it makes the common path depend on a very new external service and makes the deterministic path untestable in CI.

### 2. Guardrail gate precedes classification

The guardrail runs before the routing table: it is the cheapest possible rejection, spends no model tokens, and fails closed on the directive itself (a matching pattern blocks) but fails open on backend availability (an unreachable backend never blocks classification).

- **Alternative considered:** classify first, then guard. Rejected — that routes a dangerous directive into a lane/model before rejecting it, which is exactly the cost we want to avoid.

### 3. Implementation surface is a stdlib-only Python 3 CLI with a routable contract

`tools/agent-routing/route.py` accepts task text via stdin or a positional argument, emits `--json` output, and uses exit codes as the contract: `0` = allow, `3` = guardrail block, `2` = usage error. Stdlib-only means no install step and no network; the tool runs anywhere Python 3 is present and is trivially unit-testable.

- **Alternative considered:** a Go binary wired into the gateway. Rejected — overkill for a decision layer and couples it to the Go toolchain; Python stdlib keeps the surface dependency-free and testable with `python3 -m unittest`.

### 4. Routing table (design of record)

The table below is the policy snapshot this change ships. Matches are case-insensitive; rules are evaluated in order, first match wins; unmatched text falls back to the `default` row. Explicit rule match reports confidence `0.9`; the `default` row reports `0.4`.

| rule name | matches (case-insensitive) | lane | model | variant |
|---|---|---|---|---|
| consensus | opinion, tradeoff/trade-off, which approach, design decision, architecture, review the design, adr | council | litellm/deepseek-v4-pro | high |
| architecture-risk | refactor across, migration, schema change, backwards compat, security, race condition, leak, flaky infra, root cause, persistent bug, third failure | oracle | litellm/deepseek-v4-pro | high |
| research-external | docs, upstream, library, api reference, how does … work, changelog, sdk, latest version | librarian | litellm/deepseek-v4-flash | low |
| recon | find, locate, where is, which files, grep, search the codebase, list all, trace | explorer | litellm/deepseek-v4-flash | low |
| ui-design | ui, ux, layout, styling, css, responsive, animation, component look, design polish, dark mode | designer | litellm/deepseek-v4-flash | low |
| implementation | implement, add endpoint, fix, write code, patch, update the, rename, wire up | fixer | litellm/deepseek-v4-pro | high |
| trivial-edit | typo, rename string, bump version, one-line, small edit, comment, changelog entry | fixer | litellm/deepseek-v4-flash | low |
| default | (no rule matched) | fixer | litellm/deepseek-v4-flash | low |

Precedence constraints (recorded explicitly): `consensus` evaluates before `architecture-risk`; `architecture-risk` evaluates before `implementation` and before `trivial-edit`. These constraints are tested, not left to table order alone.

### 5. Guardrail pattern categories (design of record)

A task that matches any of these categories with high confidence is blocked with human-readable reasons and yields no lane/model:

- Ignoring or overriding prior system-or-developer instructions.
- Revealing the system prompt.
- Secret exfiltration: `.env`, private keys, `API_KEY`, tokens, credentials being read and sent somewhere.
- Remote-download piped into a shell: `curl … | bash`, `wget … | sh`.
- Destructive filesystem/DB commands: `rm -rf /`, `DROP TABLE`, `TRUNCATE`, unbounded mass delete.
- Bypassing safety rails: `--no-verify`, force-push to main, disabling CI/tests to make a gate pass.

A task that merely references one of these patterns in order to fix or guard against it (remediation) is **not** blocked — the guardrail distinguishes "do the dangerous thing" from "protect against the dangerous thing".

### 6. Escalation policy

A blocked task is surfaced to the user and never silently retried. The same problem failing 2+ times, or an ambiguous classification, escalates to the oracle/council lane. This policy lives in the `AGENTS.md` policy section, not just in the tool.

## Alternatives Rejected

- **Hosted Jev as the decision backend** — proprietary, no OpenAI-compatible surface, external dependency plus cost, and it cannot be an agent model. Its non-compatible API means it cannot replace the lane model in OpenCode/Paseo; it could only ever be a sidecar, and the external-dependency/cost trade-off is not worth it while a self-hostable Apache-2.0 option exists.
- **An LLM-based router** — cost, latency, non-determinism, and untestable. Using an LLM to decide which LLM to call reintroduces the token cost and flakiness the decision layer exists to remove.

## Risks / Trade-offs

- **Laya is a very new project** → single maintainer and an unverified adoption spike. Mitigation: treat it as optional; keep the heuristic path authoritative; never gate a decision on backend availability.
- **Routing table is a policy snapshot** → it will drift as lanes/models change. Mitigation: keep the table as data, unit-test precedence explicitly, and update the table in the same change that renames lanes/models.
- **Guardrail regexes have false positives/negatives** → they are a speed bump, not a security boundary. Mitigation: remediation allowance for false positives; surface (never silently retry) blocks; document that real security enforcement stays outside this layer.
- **Thresholds not yet calibrated** → confidence thresholds and match scoring are uncalibrated against real traces. Mitigation: ship explicit defaults (0.9 / 0.4), expose them as data, and treat calibration as follow-up.

## Migration Plan

1. Ship the four spec artifacts (this change) — proposal, design, tasks, delta spec.
2. Implement `tools/agent-routing/route.py` (stdlib-only) and its deterministic unit tests; verify with `python3 -m unittest` and `ruff check tools/agent-routing`.
3. Wire the workflow: add `.opencode/command/route-task.md` and the `AGENTS.md` policy section, then consult the decision layer before dispatching each task.
4. Optionally enable the Laya backend via environment/configuration; confirm degradation by pointing the endpoint at an unreachable address and observing the deterministic fallback.
5. Rollback: remove the wiring (`.opencode/command/route-task.md`, the `AGENTS.md` section) and stop invoking the tool; the tool itself is additive and inert when not called.

## Open Questions

- Whether to adopt hosted Jev later for higher-precision decisions once the Laya path is proven — deferred; Jev is documented here only as the hosted alternative.
- Calibration of confidence thresholds against real production traces (deferred follow-up).
