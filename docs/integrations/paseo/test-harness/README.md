# Isolated Paseo test instance for the Memory ACP provider

A self-contained harness that stands up a **second, isolated Paseo daemon** for
exercising the `memory` ACP provider without touching the live daemon at
`~/.paseo` (which listens on `0.0.0.0:6767`).

The test instance:

- uses its own home dir (`/root/paseo-acp-test` by default),
- listens on its own loopback port `127.0.0.1:6768`,
- runs with browser tools, MCP injection, relay, and the web UI disabled,
- fronts the `memory` provider through a dedicated wrapper
  (`memory-acp-test-wrapper.sh`) that loads a **dedicated** env file
  (`$TEST_HOME/memory-acp.env`) — never the operator's live
  `~/.memory/memory-acp.env`.

## Files

| File | Purpose |
|---|---|
| `setup.sh` | Create home dir, config, wrapper, and an env-file stub |
| `teardown.sh` | Stop (and optionally wipe) the test instance |
| `config.json` | Config template (listen `127.0.0.1:6768`, side effects off) |
| `memory-acp-test-wrapper.sh` | Wrapper loading the dedicated env file |

## Commands

```bash
# 1. stand up the instance (creates home + config + wrapper + env stub)
./setup.sh

# 2. fill in the dedicated env file (never commit it; chmod 600)
#    $PASEO_ACP_TEST_HOME/memory-acp.env  (default /root/paseo-acp-test/memory-acp.env)
#    keys: MEMORY_SERVER_URL, MEMORY_PROJECT_TOKEN, MEMORY_PROJECT_ID, MEMORY_AGENT

# 3. start the test daemon
./setup.sh --start                       # or: paseo daemon start --home "$PASEO_ACP_TEST_HOME"

# status / stop / teardown
paseo daemon status --home "$PASEO_ACP_TEST_HOME"
paseo daemon stop   --home "$PASEO_ACP_TEST_HOME"
./teardown.sh                            # stop only
./teardown.sh --wipe                     # stop + delete the test home
```

Override the home and port:

```bash
PASEO_ACP_TEST_HOME=/root/paseo-acp-test PASEO_ACP_TEST_LISTEN=127.0.0.1:6768 ./setup.sh
```

## Verify the provider wiring

```bash
paseo --home "$PASEO_ACP_TEST_HOME" provider ls              # memory ... available
paseo --home "$PASEO_ACP_TEST_HOME" provider diagnostic memory
# -> ACP spawn: ok / initialize: ok / session/new: ok / cleanup: ok / Status: Ready

# full end-to-end smoke (run with the caller-session vars unset so the CLI does
# not try to attach to the live daemon's caller agent):
env -u PASEO_AGENT_ID -u PASEO_HOME -u AGENT -u PASEO_AGENT_CWD \
  paseo --home "$PASEO_ACP_TEST_HOME" run --provider memory -d "Reply with exactly: PONG"
```

## Isolation guarantees

- The test daemon is addressed exclusively via `--home "$PASEO_ACP_TEST_HOME"`;
  it never reloads or restarts the live daemon, and `~/.paseo/config.json` is
  never written (verify: `sha256sum ~/.paseo/config.json` before and after).
- Ports are distinct: live `0.0.0.0:6767`, test `127.0.0.1:6768`.
- No tokens are committed; the env file is generated as a 0600 stub you fill in.
