# Memory — Infrastructure Guide

Current state of the Memory voice-assistant infrastructure (last updated 2026-08-12).

## Architecture

```
┌────────────────────┐        ┌─────────────────────────────┐
│  Client (voice)    │  WS    │  LiveKit Server (self-host) │
│  Mac Mini          │ ─────► │  home2 LXC 112              │
│  wakeword_client   │        │  livekit-server v1.9.2      │
└────────────────────┘        │  + redis 7 (tailnet-only)   │
        ▲                     └──────────────┬──────────────┘
        │ wake word                          │ WS / WebRTC (UDP range)
        └─── participants join room ◄────────┘
                                              │
                     ┌────────────────────────▼────────────────────────┐
                     │     Agent worker (home2 LXC 112, systemd)      │
                     │  main_google_realtime → gemini realtime (3.1)  │
                     │  memory-google-rt.service, Restart=always      │
                     └──────────────────┬─────────────────────────────┘
                                        │ LAN
                                   HA-MCP backend
                                   ha-home (10.100.1.4)
```

## Where things run

| Component | Host | Tailscale IP | LAN IP | Details |
|-----------|------|-------------|--------|---------|
| LiveKit server (+ redis) | home2 (LXC 112 `livekit`) | `100.69.175.118` | `10.100.1.23` | Docker compose in `/opt/livekit`, host networking, UFW tailnet-only |
| Agent worker | home2 (LXC 112) | — | localhost | `memory-google-rt` via systemd, see below |
| Voice client (wake word) | mcj-mini-2-1 (Mac Mini) | `100.121.213.124` | — | launchd `com.memory.google` |
| HA-MCP backend | ha-home | `100.112.213.75` | `10.100.1.4` | `HA_MCP_URL` / `HA_TOKEN` |
| LiteLLM gateway | zoidberg2 LXC 193 `litellm` | — | `10.10.10.93` | `LLM_BASE_URL`, LLM = `deepseek-v4-flash` |
| Memory service | runs with agent | localhost | — | `MEMORY_URL=http://localhost:5300`, MCP credentials in `.env` |

### Dev test MCP servers (api.dev)

Registered in the gateway dev project (`bf10f0e4…`) so the MCP Servers UI and e2e suites
have reachable external servers:

| Server | URL | Auth | Notes |
|--------|-----|------|-------|
| `exa` | `https://mcp.exa.ai/mcp` | none (keyless) | 2 tools (`web_fetch_exa`, `web_search_exa`); `E2E_MCP_EXAMPLE_URL` target |
| `firecrawl` | `https://mcp.firecrawl.dev/mcp` | none for sync/list; `Authorization: Bearer <free key>` for tool calls | 3 tools keyless; add key for full toolset/calls (see `docs/tasks/dev-firecrawl-mcp-key.md`) |

- Cloud dev memory reaches **public internet** only; tailnet/self-hosted test servers do not
  work there. The `memory-self` self-loop (`…/api/mcp` + token header) is local-docker only.
- Agent tool resolution requires memory ≥ the release containing PRs #406/#410, else
  whitelisted tools never reach the model.

## LiveKit server

- **Address**: `ws://100.69.175.118:7880` (also `livekit.<tailnet>.ts.net` MagicDNS)
- **Ports**: 7880 TCP signaling · 7881 TCP RTC · **UDP 50000–60000** RTC media
- **Key**: `devkey` / `secretsecretsecretsecretsecret12` (also in infrastructure repo: `vault/secrets.yml` → `vault_livekit_api_key` / `vault_livekit_api_secret`)
- **Config**: `/opt/livekit/livekit.yaml` — `rtc.node_ip` = container Tailscale IP (required for cross-network media)
- **Manage**: `ssh root@home2` → `pct exec 112 -- docker compose -f /opt/livekit/docker-compose.yml ps|logs|up -d`
- Access is **Tailscale-only**: UFW allows `100.64.0.0/10`, everything else denied. No TURN — all endpoints are on the tailnet.

## Agent worker (home2 LXC 112, /root/alfred)

| Worker | Start | Log | Env file | Model |
|--------|-------|-----|----------|-------|
| Google Realtime | systemd `memory-google-rt.service` | `journalctl -u memory-google-rt` | `.env.google` | `gemini-3.1-flash-live-preview` |

- Runs **inside the livekit LXC 112**: `LIVEKIT_URL=ws://localhost:7880`, `HA_MCP_URL=http://10.100.1.4:9583/...` (LAN, no tailscale hop).
- systemd `Restart=always` + LXC `onboot=1` → auto-recovers from crash and reboot.
- `SESSION_LOG=/tmp/memory-sessions.jsonl` for structured per-session records.
- Retired 2026-08-12 (code still in repo, not running): deepgram/cartesia `main.py`, xAI `main_realtime.py`.

## iOS development host

- **`mcj@mcj-mini-2-1`** (Mac Mini) is the iOS dev host — Swift/Xcode work happens there, never on this Linux server (no Xcode).
- Sync `client/ios/` via rsync and build with `xcodebuild` there (`./deploy.sh` does both).

## Client (Mac Mini, ~/alfred)

- launchd job `com.memory.google` → `.venv/bin/python -m client.wakeword_client`, `MEMORY_ENV=.env.google`
- Restart: `launchctl kickstart -k gui/$(id -u)/com.memory.google` · Log: `/tmp/memory-google-client.log`
- Plist: `~/Library/LaunchAgents/com.memory.google.plist`

## Memory macOS connector app

The desktop connector (`Memory.app`, SwiftUI + embedded Go engine) signs in with
Zitadel and relays local Apple MCP tools to Memory. Built-in environments:

| Env | Memory server | Zitadel issuer | Native client id |
|---|---|---|---|
| Prod | `https://memory.emergent-company.ai` | `https://auth.emergent-company.ai` | `390138006318678019` |
| Dev | `https://api.dev.emergent-company.ai` | `https://zitadel.dev.emergent-company.ai` | `390138928478289930` |

- **Sign-in:** Authorization Code + PKCE, redirect `com.emergent.memory.connector://callback`
  (both native apps must allow the scheme + post-logout URI, Development mode, refresh tokens).
- **Secrets:** 0600 files under `~/.config/memory-connector/accounts/<accountId>/`
  (`session.json`, `projects-tokens.json`, `profiles.json`, `manual-token`);
  engine config at `~/.config/memory-connector.yml`. No Keychain use.
- **Build/install (from main Mac):** `tools/mac-build.sh --install` → `~/Applications/Memory.app`
  (rsync to `mcj-mini`, xcodegen, xcodebuild, ad-hoc sign). Optional stable identity:
  `tools/mac-sign.sh` (run in GUI Terminal).
- **Engine lifecycle:** the app runs the connector only while a project is
  **Connected** (per-project toggle); stale configs are not started.
- **Tool host `tool`** (Tailscale `tool.tail0358fa.ts.net`, macOS, user `mcj`):
  `Memory.app` is installed but not signed in; reachable over SSH **from the main Mac**
  (not from this Linux server).

## Token endpoint (iOS client)

`POST http://<host>:8080/api/token` on the admin server (`agent/admin.py`, `ADMIN_PORT` default **8080**) mints a short-lived LiveKit room-join JWT server-side, so no LiveKit credentials ship in the iOS binary.

- **Method / path**: `POST /api/token` (JSON body)
- **Headers**: `X-API-Key: <TOKEN_API_KEY>` (required), `Content-Type: application/json`
- **Request body**: `{ "identity": "<participant id>", "room": "<room name>", "agent": "<optional, defaults to LIVEKIT_AGENT_NAME / memory-google-rt>" }`
- **Response 200**: `{ "server_url": "<ws url>", "participant_token": "<jwt>" }` — exactly the LiveKit Swift SDK `TokenSourceResponse` shape; `server_url` = `LIVEKIT_PUBLIC_URL` if set, else `LIVEKIT_URL`. The JWT carries `video.roomJoin=true`, `video.room`, the identity, and `roomConfig.agents` so the agent dispatches on connect.
- **Errors (no token in body)**: 400 malformed JSON / missing `identity` or `room`; 401 missing or wrong `X-API-Key`; 403 disallowed room; 503 `TOKEN_API_KEY` unset (fail closed).
- **Room allow-list**: `TOKEN_ALLOWED_ROOMS` (comma-separated) if set; otherwise only rooms prefixed `memory-`.
- **Trust boundary**: Tailscale-only (same UFW policy as the LiveKit server) **plus** the shared `TOKEN_API_KEY`. Anyone on the tailnet without the key cannot mint tokens.

## Env files — LIVEKIT_* matrix

All env files point at the self-hosted server:

```
LIVEKIT_URL=ws://100.69.175.118:7880
LIVEKIT_API_KEY=devkey
LIVEKIT_API_SECRET=secretsecretsecretsecretsecret12
```

Applies to `.env`, `.env.google`, `.env.realtime`, `.env.cloud` on **both** mcj-one and the Mac Mini.

## Dev memory backend (api.dev)

The gateway's `MEMORY_URL=https://api.dev.emergent-company.ai` points at the **`emergent-dev`** tailnet host
(`100.117.62.45`, ssh alias `emergent-dev` in `~/.ssh/config`), which runs the Emergent Memory server in Docker:

- Stack: docker compose project **`emergent-dev`** at `/opt/emergent-dev/docker-compose.yml` on the host;
  services include `memory-server` (`ghcr.io/emergent-company/memory-server:dev`), `memory-db` (pgvector:pg16), zitadel.
- Deploy flow (verified 2026-09-08): the `:dev` image is published from the **emergent-company/emergent.memory**
  repo by the `publish-self-hosted-images` workflow, which fires on **git tag pushes** (e.g. `v0.66.0`), not on
  `main` merges. The host then `docker compose pull` + recreates the container. So a merged PR reaches api.dev only
  via the next **release tag**.
- The dev server runs with test-token middleware: `Authorization: Bearer e2e-test-user` (plus `X-Org-ID` /
  `X-Project-ID` for scoped routes) is accepted, which is how the repo's Go integration suites run against it
  (`TEST_SERVER_URL`).
- Source checkouts: `/root/emergent.memory` here = the **emergent-company/emergent.memory** repo (dev commits/PRs);
  `/root/emergent` **on the dev host** is an unrelated `eyedea-io/emergent` monorepo checkout (vestigial, not the
  running server). The prod memory stack on VM 220 is separate; the `emergent-memory` ssh alias used by
  `tools/logs.sh` is configured on mcj-one, not on this box.

## Headroom proxy (dev opencode LLM compression)

Headroom (context-compression layer, `headroom-ai` PyPI, v0.37.0) runs as a local proxy on this box
(`alfred-dev`) in front of the **litellm** gateway (`100.113.48.6:4000`, tailnet host `litellm`). It
compresses opencode request traffic — tool outputs, logs, history — before it reaches the LLM, then
forwards to the gateway unchanged in every other respect (model, auth, paths). Compression runs
locally; nothing is sent to a third party. Lifetime stats so far are small because only fresh opencode
processes are routed (see "Scope" below).

- Install: `uv tool install --python 3.13 "headroom-ai[proxy,mcp]"` → `/root/.local/bin/headroom`.
- Service: **`headroom-proxy.service`** (systemd, enabled, `Restart=always`), binds `127.0.0.1:8787`.
  Upstream set via `Environment=OPENAI_TARGET_API_URL=http://100.113.48.6:4000/v1` in the unit.
  Manage with `systemctl status|restart|stop headroom-proxy`.
- Routing: opencode's global config `~/.config/opencode/opencode.json` sets
  `provider.litellm.options.baseURL = http://127.0.0.1:8787/v1`. Backup before change:
  `opencode.json.headroom-prebak`.
- **Scope:** opencode reads config at startup. Only processes started after the config change are
  routed; long-running `opencode serve` daemons (and the subagent lanes they spawn) keep the old
  baseURL until restarted.
- Viewing savings:
  - `tools/headroom-stats.py` — human-friendly lifetime + recent stats (this is the one to run
    periodically). Its "$ savings" line counts **compression only** (what Headroom actually removes).
    Provider prefix-cache reads are listed separately and never added in: prefix caching is
    provider-native (opencode/gateway get that discount with or without Headroom), so the proxy's
    lifetime `cache_savings_usd` is not Headroom's doing. Do not present it as savings.
  - `curl http://127.0.0.1:8787/stats` (live) · `/stats-history` (durable, survives restarts, stored
    at `~/.headroom/proxy_savings.json`) · `/metrics` (Prometheus).
  - `headroom dashboard` (web UI) and `/root/.local/bin/headroom savings` (CLI).
  - Logs: `journalctl -u headroom-proxy.service -f`.
- Tuning knobs (proxy env): `HEADROOM_OUTPUT_SHAPER=1` trims response tokens (off by default),
  `HEADROOM_BUDGET` caps spend. `headroom doctor` checks health.
- Revert: restore `opencode.json` from `opencode.json.headroom-prebak`, then
  `systemctl disable --now headroom-proxy.service` and delete `/etc/systemd/system/headroom-proxy.service`.

## Caveats

- **Adaptive interruption falls back to VAD** — the LiveKit Cloud `agent-gateway` bargein service is unavailable on self-hosted; agents log a 401 warning and fall back automatically.
- **LiveKit Cloud project `alfred-m0wxmmnj` is unused** since 2026-08-12 (migration to home2) — can be deleted from the LiveKit console.
- Old self-hosted stack on mcj-one (`/root/alfred/docker-compose.yml`) was **stopped** — do not start it without repointing env files back.

## Changes

- 2026-08-12: migrated LiveKit from mcj-one docker stack + LiveKit Cloud to self-hosted home2 LXC 112; unified all credentials on `devkey`; agents and client repointed; old stack retired.
- 2026-08-12: moved agent worker from mcj-one (cloud) into home2 LXC 112; model → `gemini-3.1-flash-live-preview` (thinking MINIMAL); agent→livekit on localhost + agent→ha-home over LAN; systemd persistence (`Restart=always`); retired deepgram/xai workers; removed redundant persistent dispatch; added client no-agent watchdog.
