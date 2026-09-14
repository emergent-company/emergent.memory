# 01 — Architecture

## System diagram

```
┌─ CLIENTS ────────────────────────────────────────────────────────┐
│  Web  (Go/go-daisy)  ·  iOS (voice)  ·  Mac (wake-word console) │
└──────────────┬───────────────────────────────────┬───────────────┘
               │ HTTPS REST (chat, agents, MCP)    │ ws/WebRTC (voice)
               │ X-API-Key                         │
┌──────────────▼───────────────────────────┐  ┌────▼─────────────────┐
│  GO APP  (single binary, :8080)          │  │  LiveKit (self-host)  │
│  ├ web UI (go-daisy)                     │  │  voice transport only │
│  ├ gateway API (proxy → memory)          │  └────┬─────────────────┘
│  ├ supervisor (worker lifecycle)         │       │ rtc_session
│  └ iOS token mint + QR                   │       │ dispatch(agent_name)
└──────────────┬───────────────────────────┘  ┌────▼─────────────────┐
               │ memory SDK / REST (emt_* token)│  Python VOICE BRIDGE │
               │                                │  (one process/agent) │
┌──────────────▼───────────────────────────┐  │  STT⇄chat⇄TTS         │
│  MEMORY (Emergent)                       │  └────┬─────────────────┘
│  agents · chat · MCP · skills · A2A      │       │ /api/chat/stream
│  tasks · tenancy · model config          │  ┌────▼─────────────────┐
└──────────────────────────────────────────┘  │  MEMORY (chat loop)   │
                                              └──────────────────────┘
```

**External services** (not in the Memory Compose): LiveKit (home2 LXC), Memory (:5300),
LiteLLM (:4000), Deepgram + Cartesia (cloud APIs), ha-mcp / any user MCP servers.

## Component responsibilities

| Component | Owns | Never does |
|---|---|---|
| **Memory** | agents, agent definitions, conversations, MCP registry, skills, A2A, tasks, model config, tenancy | voice (no TTS/audio/duplex) |
| **Go app** | web UI, client-facing API (proxy), worker supervisor, iOS token/QR | agent logic, durable state |
| **Voice bridge (Python)** | audio ⇄ text conversion around memory chat, turn-taking, barge-in | agent logic, config, persistence |
| **LiveKit** | real-time audio transport, rooms, agent dispatch | agent logic, STT/TTS, state |

## Ownership map — where data lives

| Data | Location |
|---|---|
| Agent definition (prompt, model, tools, skills) | Memory `AgentDefinition` |
| MCP server registry (url, transport, headers, allowlist) | Memory MCP registry |
| Chat history / messages | Memory `chat_conversations` / `chat_messages` |
| Model/provider config + credentials | Memory (encrypted at rest) |
| Agent runs / A2A dispatch state | Memory `agent_runs` / `agent_run_jobs` |
| LiveKit room/participant state | LiveKit (ephemeral) |
| Worker process state (PIDs) | Go supervisor (in-memory only) |
| iOS room-join JWT | minted by Go app, ephemeral |

## Data flows

### Text chat (web)
```
user → Go web UI → Go gateway → memory /api/chat/stream (SSE)
memory → agent loop (ADK, MCP tools) → SSE tokens → Go → browser
```

### Voice turn (iOS / Mac)
```
mic → LiveKit audio → bridge STT (Deepgram, streaming) → text
text → bridge → memory /api/chat/stream (SSE)
memory agent loop → SSE text tokens → bridge TTS (Cartesia, streaming) → LiveKit audio → speaker
```

### Ad-hoc agent creation
```
user → Go web UI → Go gateway → memory agent-definition CRUD
Go supervisor detects new enabled agent → spawns python bridge process (AGENT_NAME=x)
agent is immediately chat-capable (text) and voice-capable (bridge)
```

### A2A (deferred, memory-internal)
```
agent A (chat loop) → calls memory `trigger_agent`/`spawn_agents` → agent B run
(no Go/Python involvement)
```

## Legacy → target map (what retires)

| Legacy component | Fate |
|---|---|
| `agent/main_google_realtime.py` (Gemini realtime, prod) | **Retire** — voice becomes STT/TTS bridge |
| `agent/main.py`, `main_google.py`, `main_realtime.py`, `main_cloud.py`, `main_selfhosted.py` | **Delete** (already non-running) |
| `agent/factory.py`, `agent/api/models.py` backend unions (openai_compat/realtime/a2a) | **Retire** — memory owns the brain/model config |
| `agent/admin.py` (legacy stdlib admin: prompt editor, sessions, token, QR) | **Retire** — folded into Go app |
| `agent/api/main.py` (FastAPI control plane) + `data/agents.db` (SQLite) | **Retire** — memory is the store; Go is the gateway |
| `agent/session_lifecycle.py`, `agent/patches.py` (Gemini realtime workarounds) | **Retire** — no realtime model, no MCP in Python |
| `agent/ha_tools.py`, `agent/ha_catalog.py` (HA function tools + catalog) | **Retire** — HA becomes an MCP server registered in memory |
| `agent/seed.py` (env→SQLite seeder) | **Replace** — a memory seed/blueprint, or plain API calls |

**Kept:** `agent/worker.py` concept (one process per agent) → becomes the bridge worker;
`client/ios/*` (reused, retargeted); `client/wakeword_client.py` (Mac console).

## Failure model

- **Memory down** → Go gateway returns 503; bridge can't complete turns. Workers stay up but idle.
- **LiveKit down** → voice unavailable; web text chat unaffected.
- **Bridge worker crash** → supervisor restarts it (crash-loop backoff). Room dispatch re-attempts.
- **Cloud STT/TTS down** → bridge surfaces error; text chat unaffected.
