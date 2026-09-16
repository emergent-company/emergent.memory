## 1. Framework Helper

- [x] 1.1 Add `TriggerAgent(t *testing.T, rl *RunLog, home, projectID, agentID string) string` to `framework/agents.go`
- [x] 1.2 Guard `rl.CLI(...)` call with `if rl != nil` check
- [x] 1.3 Verify the helper compiles with `go build ./framework/...`

## 2. Replace Call Sites in orchestrator_test.go

- [x] 2.1 Replace inline trigger call at line ~63 with `framework.TriggerAgent(t, rl, home, projectID, orchestratorAgentID)` and remove the inline `rl.CLI(...)` line
- [x] 2.2 Replace inline trigger call at line ~208 with `framework.TriggerAgent`
- [x] 2.3 Replace inline trigger call at line ~421 with `framework.TriggerAgent`
- [x] 2.4 Replace inline trigger call at line ~711 (the `--output json` variant) with `framework.TriggerAgent` and keep any downstream JSON parsing on the returned string
- [x] 2.5 Replace inline trigger call at line ~1015 with `framework.TriggerAgent`
- [x] 2.6 Replace inline trigger call at line ~1306 with `framework.TriggerAgent`

## 3. Replace Call Site in task_cli_test.go

- [x] 3.1 Identify the hard (fatal) trigger call vs. the soft (error-returning) trigger call at line ~223
- [x] 3.2 Replace the hard call with `framework.TriggerAgent` and remove the inline `rl.CLI(...)` line
- [x] 3.3 Leave the soft `runCLIInDirWithHome` variant as a direct `framework.RunCLIInDirWithHome` call

## 4. Verify

- [x] 4.1 Run `go build ./...` from repo root — no compile errors
- [x] 4.2 Run `go vet ./...` — no vet issues
