## Why

`memory acp` fronts a single Memory agent, hard-wired at spawn time via `--agent` / `MEMORY_AGENT`. It never advertises which agents are available, so an ACP client such as Paseo can only offer a generic "Default" mode — it cannot enumerate the project's external agents and let the user pick one. The list exists (the extended AgentCard already exposes every `external` agent as an A2A skill), but the ACP bridge drops it on the floor.

## What Changes

- `memory acp` fetches the project's extended AgentCard at startup and derives the list of selectable agents (skill id + name + description).
- `session/new` advertises that list through the ACP session-modes surface (`modes`) and, in parallel, the newer session-config-options surface (`configOptions` with a `select` option), so both older and newer ACP clients can render a selector. The default is the configured `--agent`/`MEMORY_AGENT` skill.
- Two new methods switch the active agent for a session: `session/set_mode` (legacy modes) and `session/set_config_option` (config options). Both validate the value against the advertised list and route subsequent prompts to the chosen skill.
- `session/prompt` sends its A2A `skillId` from the per-session selection rather than the global default.

Discovery failure is non-fatal: if the card cannot be fetched, the bridge advertises a single mode (the configured default) and behaves exactly as before.

## Capabilities

### Modified Capabilities

- `cli-acp`: the ACP agent advertises the project's external agents as selectable modes/config-options on `session/new`, honours `session/set_mode` and `session/set_config_option`, and routes `session/prompt` to the per-session selected skill.

## Impact

- CLI (`apps/cli/internal/acp/`): `wire.go` (mode/config-option wire types + `SessionNewResponse` extension), `agent.go` (mode list, per-session skill, `setMode`/`setConfigOption`, prompt routing), `run.go` (dispatch `session/set_mode` + `session/set_config_option`), `acp_test.go` (regression coverage).
- CLI command (`apps/cli/internal/cmd/acp.go`): fetch the extended AgentCard and pass the derived modes into `acp.NewAgent`.
- No server, DB, or schema change: the extended AgentCard already exists.
