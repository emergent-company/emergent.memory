# Memory as an ACP provider in Paseo (`memory acp`)

This guide wires the Memory CLI into [Paseo](https://paseo.sh) as a **custom
ACP provider**, so Paseo can spawn `memory acp` and talk to a Memory agent over
the [Agent Client Protocol](https://agentclientprotocol.com/protocol/v1/) (ACP
v1, newline-delimited JSON-RPC 2.0 over stdio).

`memory acp` bridges each ACP session to a Memory agent via **A2A** — every
prompt is forwarded to the configured agent, and streamed replies are emitted
back as `session/update` → `agent_message_chunk` frames. Only JSON-RPC goes to
stdout; all diagnostics go to stderr.

- CLI source: `apps/cli/internal/cmd/acp.go`, `apps/cli/internal/acp/{wire,run,agent}.go`
- Paseo docs: <https://paseo.sh/docs/custom-providers.md>, <https://paseo.sh/docs/configuration.md>

---

## 1. Prerequisites

- Go toolchain, `task`, and `templ` available. In this environment binaries live
  in `/root/go/bin` and are **not** on `PATH` by default — prefix commands with
  `PATH="/root/go/bin:$PATH"`.
- Paseo installed and its daemon running (`paseo` on `PATH`).
- A reachable Memory server URL and a project-scoped API token (`emt_...`) with
  the `agents:write` scope, plus the skill id (RFC1123 slug) of the Memory agent
  to front. Discover the slug with `memory a2a discover`.

## 2. Install the CLI

```bash
cd <repo>
PATH="/root/go/bin:$PATH" task cli:install     # builds + installs -> ~/.memory/bin/memory
~/.memory/bin/memory --version
~/.memory/bin/memory acp --help
```

## 3. Required environment variables

`memory acp` reads connection/auth from the standard CLI config, and the agent
from `--agent` / `MEMORY_AGENT`:

| Var | Purpose |
|---|---|
| `MEMORY_SERVER_URL` | Memory server base URL, e.g. `https://api.dev.emergent-company.ai` |
| `MEMORY_PROJECT_TOKEN` | Project-scoped API token (`emt_...`). `MEMORY_PROJECT_API_KEY` is an accepted alias. |
| `MEMORY_AGENT` | Memory agent skill id to front (or pass `--agent <skill-id>`). |

Picker order for the server URL / token (see `apps/cli/internal/config/config.go`
and `apps/cli/internal/cmd/projects.go`): CLI flags → environment variables →
`~/.memory/config.yaml`.

> `memory acp` constructs its A2A client **before** it reads stdin, so a missing
> token makes the process exit with an auth error before any ACP traffic. The
> provider entry is therefore **non-functional until the operator supplies a
> real token**.

## 4. Provider configuration

Paseo's config lives at `~/.paseo/config.json` (`$PASEO_HOME/config.json`).
Per the [custom providers docs](https://paseo.sh/docs/custom-providers.md), every
custom entry under `agents.providers.<id>` needs `extends` (a built-in provider
id or the literal `"acp"`) and `label`; the id must match
`/^[a-z][a-z0-9-]*$/`; `command` is an **array** (binary first, then args).
Paseo spawns the process, sends an ACP `initialize`, and the agent reports its
capabilities, modes, and models **at runtime** — so no modes/models need to be
declared for `memory acp`.

### Recommended: wrapper script (no secret in the Paseo config)

Put the token in a private file that only the operator controls, and point
Paseo at a small wrapper that sources it and `exec`s the CLI.

`~/.memory/memory-acp-wrapper.sh` (mode `600`):

```sh
#!/bin/sh
set -eu
ENV_FILE="${MEMORY_ACP_ENV_FILE:-$HOME/.memory/memory-acp.env}"
MEMORY_BIN="${MEMORY_ACP_BIN:-$HOME/.memory/bin/memory}"
[ -f "$ENV_FILE" ] || { echo "memory-acp-wrapper: missing $ENV_FILE" >&2; exit 1; }
. "$ENV_FILE"
: "${MEMORY_SERVER_URL:?}"; : "${MEMORY_PROJECT_TOKEN:?}"; : "${MEMORY_AGENT:?}"
exec "$MEMORY_BIN" acp --agent "$MEMORY_AGENT"
```

`~/.memory/memory-acp.env` (mode `600`, operator-owned, **never committed**):

```sh
MEMORY_SERVER_URL=https://api.dev.emergent-company.ai
MEMORY_PROJECT_TOKEN=emt_<real project token>
MEMORY_AGENT=<agent skill id>
```

Provider block to merge into `~/.paseo/config.json` (into the existing
`agents.providers` object — do not replace it):

```json
"memory": {
  "extends": "acp",
  "label": "Memory",
  "command": ["/root/.memory/memory-acp-wrapper.sh"],
  "enabled": true
}
```

### Alternative: inline `env` map

Paseo's `env` map is overlaid on the daemon's inherited environment, so you can
supply the three vars directly instead of using a wrapper:

```json
"memory": {
  "extends": "acp",
  "label": "Memory",
  "command": ["/root/.memory/bin/memory", "acp"],
  "env": {
    "MEMORY_SERVER_URL": "https://api.dev.emergent-company.ai",
    "MEMORY_PROJECT_TOKEN": "emt_<real project token>",
    "MEMORY_AGENT": "<agent skill id>"
  }
}
```

**Trade-off:** the inline form is simpler but writes a live project token in
cleartext into `~/.paseo/config.json`. The wrapper keeps the secret in a
`600` file and out of the daemon config, so it is the recommended option.
`MEMORY_AGENT` is not a secret and may be passed via `command` args instead
(`["/root/.memory/bin/memory", "acp", "--agent", "<skill-id>"]`).

## 5. Apply the config

Back up, then reload — **never restart**:

```bash
cp -a ~/.paseo/config.json ~/.paseo/config.json.bak.$(date +%Y%m%dT%H%M%SZ)
# ... edit ~/.paseo/config.json ...
python3 -c "import json;json.load(open('$HOME/.paseo/config.json'));print('valid JSON')"
paseo daemon reload
```

Per the [configuration docs](https://paseo.sh/docs/configuration.md), provider
definitions are **runtime-safe**: `reload` validates the whole file, applies
runtime-safe changes, and reports any restart-required paths separately —
"edits never implicitly restart." Restart-required settings are listen
addresses, auth/passwords, relay/TLS, worktrees, service-proxy addresses, the
bundled web UI, logging, speech, voice, credentials, and local model settings;
**providers are not among them**. Use `paseo daemon restart` only if `reload`
reports a restart-required path for something you actually changed.

## 6. Post-reload validation

1. **Provider is registered** — the daemon should now list `memory`:
   `paseo provider ls`.
2. **Standalone wire check** (no Paseo). With real credentials exported, drive
   the ACP loop directly and confirm JSON-RPC on stdout:
   ```bash
   printf '%s\n' \
     '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}' \
     '{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"/tmp","mcpServers":[]}}' \
     | MEMORY_SERVER_URL=... MEMORY_PROJECT_TOKEN=... MEMORY_AGENT=... ~/.memory/bin/memory acp
   ```
   Expect an `initialize` result with `agentInfo.name == "memory"`, then a
   `session/new` result with a `sessionId`.
3. **Prompt + streaming** — launch an agent in Paseo using the `memory` provider
   and send a prompt. Expect incremental `session/update` /
   `agent_message_chunk` frames appearing as the Memory agent streams, and a
   final `stopReason: "end_turn"`.
4. **HITL resume** (`TASK_STATE_INPUT_REQUIRED`) — direct the agent to ask a
   question (or use an agent configured to pause). Expect the question text to
   be streamed, then answer in the same Paseo session: the bridge threads the
   paused `taskId` on the next prompt and the turn completes.
5. **Cancellation** — cancel a long-running turn; expect `stopReason:
   "cancelled"` rather than a hang.

## Troubleshooting

- **`memory acp` exits immediately with "Your session has expired or you are not
  authenticated"** — no token resolved. Check `MEMORY_PROJECT_TOKEN` /
  `MEMORY_PROJECT_API_KEY`, or `~/.memory/config.yaml`.
- **`no Memory agent selected`** — neither `--agent` nor `MEMORY_AGENT` was set.
- **Provider spawn fails** — validate the wrapper manually
  (`/root/.memory/memory-acp-wrapper.sh`) and confirm the file is mode `600` and
  readable by the daemon's user.
