## Why

Alfred currently runs three client-facing backends over two data stores and two web UIs:

- `gateway/` (Go): token mint, agents/MCP CRUD, chat, conversations, models — talks to Emergent Memory.
- `agent/admin.py` (stdlib): token mint, QR config, session-log, memory proxy, prompt editor — reads `SESSION_LOG` jsonl + SQLite.
- `agent/api/` (FastAPI): agents/MCP CRUD — reads SQLite `data/agents.db` (iOS hits `:8081`).

The vision (spec 00, D1/D4, principles 1–2) is one client-facing origin — the Go gateway — with Memory as the sole durable store. This change converges there: move the remaining iOS-facing endpoints (QR config, session-log, memory proxy) into the gateway, retarget iOS to the gateway, and retire the legacy Python backends, the `ui/` go-daisy module, the SQLite agent DB, and the Gemini-realtime bridge.

## What Changes

- Gateway gains three endpoint groups matching the existing client contracts:
  - `GET /api/qr-config` — the QR onboarding payload (server URL, token endpoint, apiBaseURL, API key, default agent/room).
  - `GET /api/sessions` + `GET /api/session` — the session-log API, now sourced from Memory conversations/history (the legacy `SESSION_LOG` jsonl is no longer written).
  - `GET /api/memories/capability` + `GET /api/memories` — the memory proxy (capability via the agent's MCP refs, search via Memory REST).
- iOS retargets its control-plane base URL and token endpoint to the gateway.
- Retire `agent/admin.py`, `agent/api/*` (FastAPI), `main*.py`, `factory.py`, `patches.py`, `ha_tools.py`, `ha_catalog.py`, `session_lifecycle.py`, self-hosted STT/TTS (`stt_local.py`, `tts_local.py`), `data/agents.db`, and the `ui/` module.
- Deploy: `deploy.sh` + systemd drop `alfred-admin` and `alfred-api`; the gateway (+ supervisor + web UI) is the sole service.

## Capabilities

### New Capabilities

- `gateway-client-api`: the Go gateway exposes the QR-config, session-log, and memory-proxy endpoints as the single client-facing origin.

### Modified Capabilities

- `session-log-api`, `agent-memory-api`: behavior unchanged; implementation relocated from `agent/admin.py` to the gateway (session-log now sourced from Memory, not `SESSION_LOG`).

## Impact

- Gateway (`gateway/`): new handlers + Memory client methods (search, entity-query, MCP-ref capability).
- iOS (`client/ios/`): config defaults point at the gateway.
- Deletes: `agent/admin.py`, `agent/api/`, legacy bridge + tooling, `ui/`, `data/agents.db`.
- Deploy: `deploy.sh`, `docker-compose.yml`, systemd units.
- No change to the control-plane API shape; iOS and the gateway converge on one origin.
