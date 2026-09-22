# Memory as a Paseo ACP provider

Run a Memory agent inside [Paseo](https://paseo.sh) by exposing `memory acp` as a
custom ACP provider. Paseo spawns `memory acp` over stdio; `memory acp` speaks
ACP v1 to Paseo and forwards every prompt to a Memory agent over A2A.

- `memory acp` implementation: `apps/cli/internal/cmd/acp.go`,
  `apps/cli/internal/acp/`
- Paseo custom providers: <https://paseo.sh/docs/custom-providers.md>
- Paseo configuration + reload semantics: <https://paseo.sh/docs/configuration.md>

This guide was written against Paseo `0.7.2` and Memory CLI `dev`.

## Prerequisites

- Memory CLI installed at `~/.memory/bin/memory`
  (`PATH="/root/go/bin:$PATH" task cli:install`).
- A Memory server URL and a project-scoped API token (`emt_…`) carrying the
  `agents:read` and `agents:write` scopes — the AgentCard lookup needs
  `agents:read`, and forwarding every prompt needs `agents:write`.
- A project with at least one **external-visibility agent definition** (below).
- Paseo `0.7.2`+ with the daemon running.

Run the commands below from the repository root (the `docs/integrations/paseo/`
source paths are relative to it).

## 1. Create an external-visibility agent definition

The `--agent` selector is an **AgentSkill id taken from the extended
AgentCard's `skills[]`**, and that array is sourced only from agent definitions
with `visibility = 'external'`. Only `external` definitions appear in the card,
so an `external` definition is the hard prerequisite for a selector you can
discover. (A `project`-visibility definition whose slug you already know does
resolve as a server-side fallback, but it is invisible in the card; `internal`
definitions are never callable over A2A.)

Code path:

- `apps/server/domain/agents/a2a_discovery.go:169` — `extendedAgentCard` calls
  `FindExternalAgentDefinitions`.
- `apps/server/domain/agents/repository.go:2803` — the query is
  `WHERE project_id = ? AND visibility = 'external'`.
- `apps/server/domain/agents/a2a_discovery.go:136,158` — each definition is
  mapped to a skill whose **`id` is `acpslug.FromName(def.Name)`**.
- `openspec/specs/a2a-discovery/spec.md` — "Authenticated extended AgentCard"
  requirement.

So the selector string is the **RFC 1123 slug of the definition's `name`**, not
its UUID and not a skills-domain skill.

```bash
memory agent-definitions create \
  --name "acp-probe-external" \
  --description "External-visibility ACP probe agent" \
  --system-prompt "You are a terse probe agent." \
  --flow-type single \
  --model "openai/deepseek-v4-flash" \
  --visibility external
```

> The `--model` must match a provider configured for the project. Check what is
> configured before choosing; a mismatched provider prefix fails at run time with
> `LLM credential resolution failed: no <provider> provider config found`.

Selector for the definition above: `acp-probe-external`.

## 2. Verify the selector appears in the card

`memory a2a discover` is currently broken (see "Known blockers"); read the card
directly. The A2A surface is addressed by the `X-Project-ID` header:

```bash
curl -sS \
  -H "Authorization: Bearer $MEMORY_PROJECT_TOKEN" \
  -H "X-Project-ID: $MEMORY_PROJECT_ID" \
  "$MEMORY_SERVER_URL/extendedAgentCard" | jq '.skills[].id'
# "acp-probe-external"
```

If `skills` is empty, the definition is missing or is not `external`.

## 3. Provider wrapper + config

Paseo's provider `command` must be a JSON array, and the provider id must match
`/^[a-z][a-z0-9-]*$/`. Credentials are **not** put in the Paseo config; a wrapper
sources them from a private 0600 env file.

Install the wrapper and env file (reference copies live in this directory;
run from the repository root):

```bash
install -m 0755 docs/integrations/paseo/memory-acp-wrapper.sh ~/.memory/memory-acp-wrapper.sh
install -m 0644 docs/integrations/paseo/memory-acp.env.example ~/.memory/memory-acp.env.example
cp ~/.memory/memory-acp.env.example ~/.memory/memory-acp.env
chmod 600 ~/.memory/memory-acp.env
# edit ~/.memory/memory-acp.env and fill in MEMORY_SERVER_URL /
# MEMORY_PROJECT_TOKEN / MEMORY_AGENT (+ MEMORY_PROJECT_ID / the shim vars
#   when following the "Known blockers" workaround)
```

`~/.memory/memory-acp-wrapper.sh` loads that env file and execs `memory acp`:

```sh
exec "$MEMORY_BIN" acp --agent "$MEMORY_AGENT"
```

Merge this block into `~/.paseo/config.json` under `agents.providers`
(preserving every other key):

```json
"memory": {
  "extends": "acp",
  "label": "Memory",
  "command": ["/bin/sh", "<HOME>/.memory/memory-acp-wrapper.sh"]
}
```

> **Replace `<HOME>` with your absolute home directory** — for `root` that is
> `/root`, so the reference setup uses `/root/.memory/memory-acp-wrapper.sh`.
> Paseo runs `command` directly (no shell), so `~` and environment variables in
> the path are **not** expanded; a literal `/root/.memory/...` path only works
> for the `root` operator.

> **Why `/bin/sh <script>` and not the bare script path?** The Paseo daemon's
> session-spawn path returns `EACCES` when the configured command is a file under
> `/root` (it can still *read* such files). `spawn /root/.memory/memory-acp-wrapper.sh
> EACCES`. Exec'ing `/bin/sh` (a PATH binary) and passing the wrapper as an
> argument is a workaround for that limitation, and keeps the script at the
> documented `~/.memory` location. Alternatively install the wrapper outside the
> home directory (e.g. `/usr/local/bin`) and reference it directly.

## 4. Apply with `paseo daemon reload` (never restart)

Provider definitions are runtime-safe: per
<https://paseo.sh/docs/configuration.md>, provider changes apply to future
launches without restarting the daemon. Do **not** restart the daemon — it hosts
other running agents.

```bash
paseo daemon reload          # -> "Configuration reloaded."
paseo provider ls            # -> memory   Memory   available   Enabled
paseo provider diagnostic memory
```

`provider diagnostic` reports the ACP handshake (`spawn` → `initialize` →
`session/new`). If `provider ls` shows `error`, run the diagnostic; a common
cause is the wrapper failing the `--version` probe (the reference wrapper answers
it explicitly).

## 5. End-to-end validation

> **Read "Known blockers" first.** Until the project-selector fix (#761/#762,
> shipped in PR #764) is on your build, a server that requires `X-Project-ID`
> rejects the SDK's A2A calls, so the commands below only succeed through the
> local header-injecting shim (`MEMORY_SERVER_URL` pointed at
> `http://127.0.0.1:18095`). On a build that includes #764 they work against the
> server directly.

```bash
paseo run --provider memory --title "acp-e2e" -d "Reply with exactly: PONG"
paseo logs <agentId>
```

Expected:

```
[User] Reply with exactly: PONG
PONG
```

`paseo inspect <agentId>` should report `Provider memory`, `Status idle`, and
`Capabilities Streaming: true, …`.

Because `session/prompt` is forwarded to A2A `message:stream`, reply text arrives
as `session/update` `agent_message_chunk` frames before the `end_turn` response —
i.e. streaming works with no extra configuration.

### Human-in-the-loop (unverified)

`apps/cli/internal/acp/agent.go` handles a paused A2A task
(`TASK_STATE_INPUT_REQUIRED`) by recording the task id and resuming it on the
next prompt. This path has **not** been exercised end-to-end against a live
agent, because the probe agent never pauses for input. Verify with an agent that
asks a question mid-run.

## Known blockers

1. **A2A SDK omits the project selector (issues #761 / #762).** The SDK client
   (`apps/server/pkg/sdk/a2a/client.go`) never sends `X-Project-ID`, so
   `memory a2a discover` and any A2A call made through the SDK/CLI fail with
   `project context is required (set via API token)` (streaming) or
   `[-32006] INVALID_AGENT_RESPONSE` (non-streaming). `memory acp` uses the same
   SDK client, so this affects it too.
   Until the fix ships, `docs/integrations/paseo/memory-acp-proxy.py` is a
   local header-injecting forward proxy that lets the full path be exercised.
   `MEMORY_ACP_PROXY_TARGET` is **required** — the proxy refuses to start
   without it, so credentials are never forwarded to an unintended server:

   ```bash
   # from the repository root
   MEMORY_ACP_PROXY_TARGET=https://api.dev.emergent-company.ai \
   MEMORY_PROJECT_ID=<uuid> \
     python3 docs/integrations/paseo/memory-acp-proxy.py 18095 &
   # point MEMORY_SERVER_URL at http://127.0.0.1:18095 in the env file
   # (MEMORY_PROJECT_ID / MEMORY_ACP_PROXY_TARGET may also live in that env file,
   #  which the proxy falls back to reading)
   ```

   This is a verification shim, not a production component — remove it and point
   `MEMORY_SERVER_URL` back at the real server once #761/#764 land (the fix is in
   PR #764). Paseo's provider config itself needs no change: only the env file's
   `MEMORY_SERVER_URL`, and later its removal.

2. **Daemon `EACCES` on `/root` command paths.** See step 3; use the
   `["/bin/sh", "<path>"]` form.

## Operator checklist

1. Install the CLI and confirm `memory acp --help`.
2. Confirm the project token carries `agents:read` + `agents:write`.
3. Create/reuse an **external** agent definition; copy its slug.
4. Fill `~/.memory/memory-acp.env` (600) with server URL, project token and agent
   slug; add `MEMORY_PROJECT_ID` (and, while using the shim,
   `MEMORY_ACP_PROXY_TARGET`).
5. Merge the `memory` provider block into `~/.paseo/config.json`; validate with
   `jq . ~/.paseo/config.json`.
6. `paseo daemon reload` (never restart).
7. `paseo provider ls` → `memory … available`, `paseo provider diagnostic memory`.
8. `paseo run --provider memory -d "Reply with exactly: PONG"`; check `paseo logs`.
   Until #764 ships this needs the shim, with `MEMORY_SERVER_URL` pointing at it.

## References

- `apps/cli/internal/cmd/acp.go` — `memory acp`, `--agent` / `MEMORY_AGENT`.
- `apps/cli/internal/acp/agent.go` — ACP→A2A bridge, streaming, HITL resume.
- `apps/server/domain/agents/a2a_discovery.go` — extended card + skill slug.
- `apps/server/domain/agents/repository.go` — external-visibility query.
- `apps/server/domain/agents/a2a_message.go` — `skillId` metadata resolution.
- <https://paseo.sh/docs/custom-providers.md>
- <https://paseo.sh/docs/configuration.md>
