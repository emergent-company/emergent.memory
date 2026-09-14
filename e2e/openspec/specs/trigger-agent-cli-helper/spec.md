# trigger-agent-cli-helper Specification

## Purpose
TBD - created by archiving change trigger-agent-cli-helper. Update Purpose after archive.
## Requirements
### Requirement: TriggerAgent framework helper
The framework SHALL export a `TriggerAgent` function in `framework/agents.go` that triggers an agent via `memory agents trigger <agentID> --project <projectID>` and returns the CLI output.

The function signature SHALL be:
```go
func TriggerAgent(t *testing.T, rl *RunLog, home, projectID, agentID string) string
```

It SHALL:
- Call `MustRunCLIInDirWithHome(t, "", home, "agents", "trigger", agentID, "--project", projectID)`
- Log the result via `rl.CLI("memory agents trigger "+agentID, out)` when `rl` is non-nil
- Return the raw CLI output string

#### Scenario: Successful trigger
- **WHEN** `TriggerAgent` is called with valid `home`, `projectID`, and `agentID`
- **THEN** it executes `memory agents trigger <agentID> --project <projectID>` and returns the output string

#### Scenario: Nil RunLog
- **WHEN** `TriggerAgent` is called with `rl == nil`
- **THEN** it SHALL skip the `rl.CLI(...)` log call and still return the CLI output

#### Scenario: CLI failure
- **WHEN** the `memory agents trigger` command exits non-zero
- **THEN** the test SHALL be failed immediately via `t.Fatalf` (fatal — not a soft error)

### Requirement: Call sites replaced in orchestrator_test.go
All 6 inline `mustRunCLIInDirWithHome(t, "", home, "agents", "trigger", ...)` trigger calls in `tests/blueprints/orchestrator_test.go` SHALL be replaced with `framework.TriggerAgent(t, rl, home, projectID, agentID)`.

The inline `rl.CLI(...)` log call after each trigger SHALL be removed (now handled inside the helper).

#### Scenario: Hard trigger calls replaced
- **WHEN** a test in `orchestrator_test.go` triggers an agent
- **THEN** it uses `framework.TriggerAgent` instead of inline `mustRunCLIInDirWithHome`

### Requirement: Call site replaced in task_cli_test.go (hard variant)
The 1 hard `mustRunCLIInDirWithHome(t, "", home, "agents", "trigger", ...)` call in `tests/tools/task_cli_test.go` (line ~223, the fatal variant) SHALL be replaced with `framework.TriggerAgent`.

The non-fatal `runCLIInDirWithHome` variant at the same location (which checks for errors and only warns) SHALL remain as a direct `framework.RunCLIInDirWithHome` call.

#### Scenario: Hard trigger call in task_cli_test.go replaced
- **WHEN** the task CLI test triggers an agent with a fatal expectation
- **THEN** it uses `framework.TriggerAgent`

