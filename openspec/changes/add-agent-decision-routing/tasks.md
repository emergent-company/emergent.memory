## 1. Spec artifacts

- [x] 1.1 Write `openspec/changes/add-agent-decision-routing/proposal.md`
- [x] 1.2 Write `openspec/changes/add-agent-decision-routing/design.md`
- [x] 1.3 Write `openspec/changes/add-agent-decision-routing/tasks.md`
- [x] 1.4 Write `openspec/changes/add-agent-decision-routing/specs/agent-decision-routing/spec.md`

## 2. Router + guardrail tool (`tools/agent-routing/route.py`, stdlib-only)

- [ ] 2.1 Implement `route.py` CLI contract: task text via stdin or positional arg; `--json` flag; exit code `0` = allow, `3` = guardrail block, `2` = usage error
- [ ] 2.2 Implement the deterministic routing table as ordered, case-insensitive, data-driven rules (first match wins, `default` fallback), with the precedence constraints `consensus` before `architecture-risk` and `architecture-risk` before `implementation` and before `trivial-edit`
- [ ] 2.3 Implement the guardrail gate that runs BEFORE classification: block on high-confidence dangerous-directive patterns (ignoring/overriding instructions, system-prompt reveal, secret exfiltration, `curl … | bash`/`wget … | sh`, destructive FS/DB, safety-rail bypass) with human-readable reasons
- [ ] 2.4 Implement the remediation allowance: a task that references a dangerous pattern to fix/guard against it is NOT blocked
- [ ] 2.5 Implement machine-readable JSON output containing lane, model, variant, confidence, matched rule, guardrail status, backend name, and degraded flag
- [ ] 2.6 Implement the optional Laya backend call (configured endpoint, configurable timeout) with graceful degradation: on error/timeout/malformed response, fall back to deterministic heuristics and mark the result degraded; record which backend produced the decision
- [ ] 2.7 Add a deterministic stdlib `unittest` test file `tools/agent-routing/test_route.py` covering classification, precedence, guardrail block, remediation allowance, default fallback, JSON shape, exit codes, and backend degradation (mocked, no network)

## 3. Unit tests (`tools/agent-routing/test_route.py`, deterministic, no network)

- [ ] 3.1 Test deterministic classification: representative text per rule maps to the expected lane/model/variant and confidence (`0.9` explicit, `0.4` default)
- [ ] 3.2 Test precedence: a task matching both `consensus` and `architecture-risk` resolves to `consensus`; a task matching `architecture-risk` and `implementation` resolves to `architecture-risk`
- [ ] 3.3 Test guardrail block: each dangerous-directive category blocks and yields no lane/model, with a human-readable reason and exit code `3`
- [ ] 3.4 Test remediation allowance: "fix the leaked API key", "add a guard against rm -rf", "block secret exfiltration" are NOT blocked and DO classify
- [ ] 3.5 Test default fallback and JSON output: unmatched text resolves to the `default` row; `--json` emits every required field
- [ ] 3.6 Test backend degradation: a mocked error/timeout/malformed backend response falls back to heuristics, sets `degraded: true`, and does NOT block or fail the task

## 4. Optional Laya backend integration

- [ ] 4.1 Support a configured backend endpoint (environment/configuration) and a configurable timeout; absent/unset endpoint means the backend is skipped entirely
- [ ] 4.2 Record the producing backend in the result (`"backend": "laya" | "heuristics"`) and include the `degraded` flag on fallback
- [ ] 4.3 Add a deterministic unit test asserting the backend is never required for a valid decision (no network in tests)

## 5. Workflow wiring

- [ ] 5.1 Add `.opencode/command/route-task.md` that runs `route.py` on the task text and surfaces the decision (lane/model/variant) or the guardrail block reason
- [ ] 5.2 Add an `AGENTS.md` policy section documenting the routing table, guardrail pattern categories, remediation allowance, and escalation policy (blocked tasks surface to the user, never silently retried; 2+ failures or ambiguous classification escalates to oracle/council)

## 6. Verification

- [ ] 6.1 Run `python3 -m unittest` in `tools/agent-routing` (all tests pass, no network)
- [ ] 6.2 Run `ruff check tools/agent-routing` (clean)
- [ ] 6.3 Run `openspec validate add-agent-decision-routing --strict` (passes)
- [ ] 6.4 Open a PR against `main` (spec + implementation in one PR); CI green and review passed before merge
