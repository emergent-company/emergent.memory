## Why

Memory retired its in-house ACP interface in favour of A2A v1.0 (#643). Agent orchestrators such as Paseo, Claude Code, and Gemini CLI can only spawn custom agents that speak the Agent Client Protocol (ACP) v1 over stdio — they cannot speak A2A. There is no way to front a Memory agent from these tools today.

## What Changes

- Add a `memory acp` command that runs the CLI as an ACP v1 agent over stdio (newline-delimited JSON-RPC 2.0).
- Implement the baseline ACP methods — `initialize`, `session/new`, `session/prompt`, and the `session/cancel` notification — and emit `session/update` to stream agent replies.
- Bridge each prompt to the Memory A2A surface (`POST /message:send`), selecting the agent by skill id via `--agent` / `MEMORY_AGENT`, and thread multi-turn conversation via the A2A `contextId`.

## Capabilities

### New Capabilities
- `cli-acp`: run the Memory CLI as an ACP v1 stdio agent that fronts a Memory agent.

### Modified Capabilities
<!-- none -->

## Impact

- `apps/cli/internal/acp/`: new dependency-free ACP wire + agent package (`wire.go`, `agent.go`, `run.go`) and tests (`acp_test.go`).
- `apps/cli/internal/cmd/acp.go`: new `memory acp` command (group `ai`), reusing `getA2AClient` for config/auth.
