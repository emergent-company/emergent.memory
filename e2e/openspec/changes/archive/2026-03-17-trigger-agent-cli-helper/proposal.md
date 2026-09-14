## Why

Agent triggering is currently done inline in every test with 7 repetitive `mustRunCLIInDirWithHome(t, "", home, "agents", "trigger", agentID, "--project", projectID)` call sites spread across two test files. Extracting this into a shared framework helper eliminates the duplication, aligns with how `PollUntilSuccess` and `CreateAgent` are handled, and makes the trigger step a single named function that tests can call consistently.

## What Changes

- Add `TriggerAgent(t, rl, home, projectID, agentID string) string` to `framework/agents.go` — wraps `MustRunCLIInDirWithHome` with `"agents", "trigger", agentID, "--project", projectID` and logs the trigger event to the run log
- Replace all 7 inline `mustRunCLIInDirWithHome(..., "agents", "trigger", ...)` call sites in `tests/blueprints/orchestrator_test.go` (6 sites) and `tests/tools/task_cli_test.go` (1 site) with calls to `framework.TriggerAgent`
- The non-fatal variant at `task_cli_test.go:223` uses `runCLIInDirWithHome` (returns error); the new helper uses `MustRunCLIInDirWithHome` (fatal on failure) — the one non-fatal call site will keep its existing pattern or use a soft variant if needed

## Capabilities

### New Capabilities
- `trigger-agent-cli-helper`: Framework helper `TriggerAgent` in `framework/agents.go` that triggers an agent via the `memory agents trigger` CLI command and logs the result

### Modified Capabilities

## Impact

- `framework/agents.go`: new exported function `TriggerAgent`
- `tests/blueprints/orchestrator_test.go`: 6 inline trigger calls replaced
- `tests/tools/task_cli_test.go`: 1 inline trigger call replaced (non-fatal variant may stay as `RunCLIInDirWithHome` direct call or use a soft helper)
- No API or dependency changes — CLI-only
