## Context

The test suite triggers agents in multiple places using a raw 3-line pattern:

```go
triggerOut := mustRunCLIInDirWithHome(t, "", home,
    "agents", "trigger", agentID,
    "--project", projectID,
)
rl.CLI("memory agents trigger "+agentID, triggerOut)
```

This appears 7 times across two test files. The framework already exports `PollUntilSuccess` for the polling phase; triggering has no equivalent named helper. The AGENTS.md docs list `TriggerAgent` as an export of `framework/agents.go` but it is not yet implemented.

## Goals / Non-Goals

**Goals:**
- Add `TriggerAgent` to `framework/agents.go` matching the documented signature
- Replace all 7 inline call sites with the new helper
- Keep the non-fatal variant at `task_cli_test.go:223` working (it currently uses `runCLIInDirWithHome` directly and tolerates failure)

**Non-Goals:**
- Changing behavior of the trigger command itself
- Adding output parsing or run-ID extraction from the trigger response
- Supporting the `--output json` flag variation as a separate helper (the one JSON site can call the helper and re-parse if needed, or remain inline — callers decide)

## Decisions

**Signature: `TriggerAgent(t *testing.T, rl *RunLog, home, projectID, agentID string) string`**
- Matches the pattern of `PollUntilSuccess` — takes `t`, `rl`, `home`, `projectID`, `agentID`
- Returns the raw CLI output string (same as `MustRunCLIInDirWithHome`)
- Uses `MustRunCLIInDirWithHome` internally (fatal on non-zero exit) — all hard call sites want this
- Logs via `rl.CLI(...)` — consistent with existing usage in call sites

**The non-fatal call site in `task_cli_test.go:223`:**
- That site needs error tolerance. It currently checks `triggerErr != nil` and only warns.
- Options: (a) keep it as direct `RunCLIInDirWithHome` call, (b) add a second helper `TryTriggerAgent` returning `(string, error)`.
- Decision: keep it as a direct `framework.RunCLIInDirWithHome` call. One site does not justify a second helper, and the intent ("trigger may fail if no provider") is already clear in a comment.

**No `srv`/`token` parameters:**
- The CLI reads config from `home` (set up by `SetupCLIAuth`). No server URL or token passed at call time.
- Consistent with how all other CLI helper calls work in the framework.

## Risks / Trade-offs

- [Risk] The `rl` parameter requires a `*RunLog`; call sites without a `RunLog` must pass `nil`. → Mitigation: guard with `if rl != nil` in the helper, same pattern used elsewhere.
- [Risk] The one `--output json` variant (orchestrator_test.go:711) won't naturally use the new helper if JSON parsing is needed. → Mitigation: the helper returns the raw string; callers can parse it. The helper logs via `rl.CLI` before returning, so the JSON parsing site can call `TriggerAgent` and then unmarshal the returned string.
