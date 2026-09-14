# 13 — Roadmap

## Strategy

**Rip-and-replace (D15).** Retire the legacy Gemini-realtime stack now; the new architecture
is the sole path. No side-by-side window — voice is unavailable until P2 (the bridge) lands,
so sequence P0→P2 before polishing the UI.

## Dependency order

```
P0 Memory provisioning ─► P1 Go gateway+supervisor ─┬─► P2 Python bridge ─► P4 clients ─► P5 cut over
                                                    └─► P3 Web UI ────────┘
```

## Phases

### P0 — Memory provisioning (foundation)

**Goal:** single-owner memory ready; the 2 existing agents live as definitions.

- Configure memory for single-owner: one org/project, one `emt_*` token (scopes:
  `agents:read/write chat:use data:read/write schema:read/write projects:read/write admin`).
- Provider credentials (org-level, encrypted): LiteLLM → `deepseek/deepseek-v4-flash`; Gemini optional.
- Seed agent definitions: `alfred` (PL prompt, HA tools) + `diane` (EN prompt, memory tools);
  register `ha-mcp` + `memory` MCP servers (with tool allowlists).
- **Gate:** `curl` `POST /api/chat/stream` against each agent returns streaming tokens.

### P1 — Go gateway + supervisor (thin backend)

**Goal:** the Go binary talks to memory and manages worker lifecycles.

- Skeleton: `echo` + `go-daisy` + memory Go SDK. One binary.
- Gateway API: agent CRUD → memory; MCP-server CRUD → memory; conversation CRUD; chat relay
  (SSE pass-through); providers/model config; iOS token mint + QR.
- Supervisor: reconcile loop (enabled agents ↔ child processes), spawn/stop/restart, crash
  backoff. Stub bridge first.
- Auth (`X-API-Key`), pairing endpoint (from Diane pattern).
- **Gate:** `go build ./...` + `go vet`; unit tests (fake memory SDK); CRUD round-trips against
  memory; supervisor reconcile tested with a fake process.

### P2 — Python voice bridge

**Goal:** voice transport = STT/TTS around the memory chat loop.

- LiveKit `AgentServer` worker (one process per agent, spawned by supervisor).
- Deepgram STT (streaming) → memory `/api/chat/stream` → Cartesia TTS (streaming).
- Turn-taking (VAD + turn-detector), barge-in (AEC), away watchdog, exit keywords,
  `lk.agent.ready` signal; per-client language (D13).
- **Gate:** pytest + mocked STT/TTS/chat; manual voice smoke against a seed agent (EN + PL).

### P3 — Web UI

**Goal:** the main user UI.

- go-daisy pages: Agents, **MCP servers** (delivered 2026-09-09 — registry CRUD, sync/
  inspect, per-tool enable, agent attach), Chat (streaming), Sessions, Providers/Models,
  Settings.
- **Gate:** `go build`; manual text chat + ad-hoc agent create via UI.

### P4 — Clients

- **iOS:** adapt the Diane shell (models/components/nav, see 11-reuse-from-diane.md) +
  retarget the existing Memory iOS voice path; build chat UI + session views (Diane has none);
  per-client language.
- **Mac:** retarget `client/wakeword_client.py` to the new bridge.
- **Gate:** full voice call via iOS and Mac against the new bridge.

### P5 — Retire legacy

- Retire: `main_google_realtime.py`, `main*.py`, `factory.py`, `admin.py`, `agent/api/*`
  (FastAPI), `data/agents.db`, `patches.py`, `ha_tools.py`, `ha_catalog.py`,
  `session_lifecycle.py`, self-hosted STT/TTS extras.
- **Gate:** new path is the sole voice path; `go build` + pytest + lint green; legacy deleted.

### P6 — Post-v1 (deferred)

Skills (D10), A2A (D9), web voice (D5).

## Milestone dependencies

| Phase | Depends on |
|---|---|
| P0 | — (memory already running) |
| P1 | P0 |
| P2 | P1 (supervisor spawns the bridge) |
| P3 | P1 (gateway API) |
| P4 | P2 + P3 |
| P5 | P4 validated |
| P6 | P5 |

## Risks

| Risk | Phase | Mitigation |
|---|---|---|
| Cascaded voice latency (vs realtime) | P2 | token-level streaming → early TTS; tune endpointing; measure e2e before cut over |
| Cartesia Polish TTS quality | P2 | verify PL voice early; fall back to another voice/model if poor |
| MCP secrets plaintext in memory | P0 | tailnet-only; don't store master creds as MCP headers |
| Cold-start dispatch window | P1/P2 | pre-warm workers on agent create |
