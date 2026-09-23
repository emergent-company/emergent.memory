## Purpose

A cross-cutting decision layer that runs before the LLM on every agent task: it gates dangerous-directive tasks, deterministically classifies each task into a lane/model/variant using ordered data-driven rules, optionally delegates to a local decision backend (Laya) when available, degrades gracefully to deterministic heuristics on any backend failure, and emits machine-readable JSON.

## ADDED Requirements

### Requirement: Deterministic classification

The decision layer SHALL classify a task description into a lane, a model, and a reasoning variant using ordered, data-driven rules, where the first matching rule wins and unmatched text falls back to a documented default lane/model/variant.

#### Scenario: Explicit rule match

- **WHEN** a task description matches one of the routing rules (for example, a task containing "implement" matches the `implementation` rule)
- **THEN** the decision reports that rule's lane, model, and variant, and a confidence of `0.9`

#### Scenario: Unmatched text falls back to default

- **WHEN** a task description matches no routing rule
- **THEN** the decision falls back to the documented `default` lane/model/variant and reports a confidence of `0.4`

#### Scenario: Rule precedence is honored

- **WHEN** a task description matches more than one rule
- **THEN** the highest-precedence rule wins (for example, `consensus` beats `architecture-risk`, and `architecture-risk` beats `implementation` and `trivial-edit`)

### Requirement: Guardrail gate runs before classification

The guardrail gate SHALL run before classification, and a task matching a high-confidence dangerous-directive pattern SHALL be blocked with human-readable reasons and yield no lane/model.

#### Scenario: Dangerous directive is blocked

- **WHEN** a task matches a high-confidence dangerous-directive pattern (for example, `curl … | bash`, `rm -rf /`, revealing the system prompt, or exfiltrating credentials)
- **THEN** the task is blocked, the output contains human-readable reasons, no lane/model/variant is produced, and the process exits with the guardrail-block code (`3`)

#### Scenario: Gate precedes routing

- **WHEN** a task both matches a dangerous-directive pattern and matches a routing rule
- **THEN** the guardrail block wins and no routing decision is produced (the gate runs first, before any classification)

### Requirement: Remediation allowance

A task that merely references a dangerous pattern in order to fix or guard against it SHALL NOT be blocked.

#### Scenario: Fixing or guarding is allowed

- **WHEN** a task references a dangerous pattern for remediation (for example, "fix the leaked API key", "add a guard against `rm -rf`", or "block secret exfiltration")
- **THEN** the task is NOT blocked and a normal lane/model/variant decision is produced

### Requirement: Optional Laya backend

When a local decision backend endpoint is configured and reachable, the classification and guardrail decisions MAY be delegated to it, and the result MUST report which backend produced the decision.

#### Scenario: Backend produces the decision

- **WHEN** a backend endpoint is configured and reachable and the decision is delegated to it
- **THEN** the result reports that backend as the producer of the decision

#### Scenario: No backend configured

- **WHEN** no backend endpoint is configured
- **THEN** the deterministic heuristics produce the decision and the result reports the heuristics as the producer

### Requirement: Graceful degradation

Any backend error, timeout, or malformed response MUST fall back to the deterministic heuristics, MUST mark the result as degraded, and MUST NOT block or fail the task.

#### Scenario: Backend failure falls back

- **WHEN** the backend errors, times out, or returns a malformed response
- **THEN** the deterministic heuristics produce the decision, the result is marked degraded, and the task is neither blocked nor failed

### Requirement: Machine-readable output

A JSON representation of the decision SHALL be available for automation, containing the decision fields (lane, model, variant, confidence, matched rule, guardrail status) and the backend and degraded status.

#### Scenario: JSON output for automation

- **WHEN** the decision layer is invoked with the machine-readable output flag (`--json`)
- **THEN** the output is valid JSON containing the lane, model, variant, confidence, matched rule, guardrail status, the producing backend, and the degraded flag
