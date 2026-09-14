# 05 — Voice Bridge (Python)

The voice bridge is the **only** Python that remains. It converts audio ⇄ text around
memory's chat loop. It contains **no agent logic, no config store, no persistence**.

- **Stack:** `livekit-agents` (transport), Deepgram STT (streaming), Cartesia TTS
  (streaming), memory Chat API (streaming text).
- **Model:** one process per agent (LiveKit SDK constraint — one `rtc_session` per process),
  spawned as a child process by the Go supervisor (D2).

## Pipeline

```
LiveKit mic audio ──► Deepgram STT (streaming, interim) ──► user text
                                                            │
                                         POST memory /api/chat/stream (SSE)
                                                            │
                                              agent loop (ADK + MCP tools)
                                                            │
LiveKit speaker ◄── Cartesia TTS (streaming) ◄── SSE text tokens
```

## Responsibilities

| Task | Implementation |
|---|---|
| Join LiveKit room (agent identity) | `livekit-agents` `rtc_session`, dispatched by room config |
| STT | Deepgram `nova-3`, streaming, language from agent config |
| Send text to brain | memory `/api/chat/stream` with `conversationId` + `AgentDefinitionId` |
| Stream tokens back | consume SSE `TokenEvent`s |
| TTS | Cartesia `sonic-3.5`, streaming; language + voice from agent config |
| Turn-taking | Silero VAD + local turn-detector (no LiveKit Cloud bargein service) |
| Barge-in | client AEC (`open_input(enable_aec=True)`); don't discard mic audio while speaking |
| Endpointing | `ENDPOINT_MIN_DELAY` / `ENDPOINT_MAX_DELAY` guards |
| Exit keywords | server-side `conversation_item_added` match (`stop`/`goodbye`/…), whole-word |

## Memory binding

The bridge must resolve **which memory agent** it drives. The 3-way identity (LiveKit dispatch
name == agent name == worker `AGENT_NAME`) is not enough — memory's chat API targets an
**agent definition id**, not a name. The supervisor therefore resolves name→id and injects it:

| Env | Set by | Purpose |
|---|---|---|
| `AGENT_NAME` | supervisor | LiveKit dispatch name + worker identity |
| `AGENT_DEFINITION_ID` | supervisor | memory `AgentDefinition` id to chat with |
| `AGENT_LANGUAGE` | supervisor | canonical ISO 639-1 code; STT + TTS follow it |
| `MEMORY_URL`, `MEMORY_TOKEN` | supervisor | memory endpoint + scoped token |
| `MEMORY_PROJECT_ID` | supervisor | memory project scope |

The bridge holds **no agent config** — it reads nothing from memory except the chat stream.
Model, prompt, tools all live in memory's agent definition.

## Conversation mapping

Each LiveKit voice session maps to **one fresh memory conversation** (D14):

- On session start the bridge creates a new conversation (no resume across sessions).
- The bridge passes that `conversationId` on every `/api/chat/stream` call so history is
  continuous *within* the session.
- Long-term recall is opt-in per agent via memory tools (see 03-agent-model.md), not via
  conversation resume.

## Language (per-agent)

STT/TTS language is an **agent** setting (D13). The agent definition's `Config["language"]`
stores a canonical ISO 639-1 code; the supervisor injects it as `AGENT_LANGUAGE`; the bridge
maps it to Deepgram STT (`language=…`) and Cartesia TTS (`language=…` + a fixed voice via
`CARTESIA_VOICE`). Both providers accept the same ISO code today — the mapping table in
`config.py` (`LANGUAGES`) is the seam where a provider-specific code goes if one diverges.
Default English; empty means auto/global fallback.

## Session lifecycle & signals

- **Ready:** after `ctx.connect()` and mic subscription, the bridge emits
  `send_text("ready", topic="lk.agent.ready")` so the client plays the cue-to-speak chime
  at the right moment (not before audio is actually subscribed).
- **Away:** activity-tracker driven by conversation turns → after `USER_AWAY_TIMEOUT`
  silence, say goodbye and shut down. (Replaces the realtime model's native activity
  detection, which no longer applies.)
- **Usage/metrics:** consume `session_usage_updated` / `ChatMessage.metrics`; log
  transcription/end-of-turn/ttft/ttfb/e2e latency.

## Retired behaviors (no longer needed)

- **Gemini realtime model + its monkey-patches** (`patches.py`: session-resumption strip,
  schema-key strip, `MCPToolset.aclose` timeout) — the bridge runs no model and no MCP in
  Python; memory handles both.
- **Home Assistant function tools / device catalog** (`ha_tools.py`, `ha_catalog.py`) — HA
  is an MCP server registered in memory now.
- **Factory backend unions** (`openai_compat`/`realtime`/`a2a`) — memory owns the model.

## Env (per worker, templated by supervisor)

| Var | Purpose |
|---|---|
| `AGENT_NAME` | LiveKit dispatch name + worker identity |
| `AGENT_DEFINITION_ID`, `MEMORY_PROJECT_ID` | memory binding (see above) |
| `MEMORY_URL`, `MEMORY_TOKEN` | memory endpoint + scoped token |
| `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` | LiveKit transport |
| `DEEPGRAM_API_KEY` | STT |
| `CARTESIA_API_KEY`, `CARTESIA_VOICE` | TTS |
| `USER_AWAY_TIMEOUT`, `EXIT_KEYWORDS`, endpointing knobs | turn/away tuning |

## Worker process (as-built)

- Run as `python -m memory_bridge start` — the livekit CLI **requires a subcommand**; bare
  `python -m memory_bridge` just prints usage.
- `AgentServer(port=0)` — ephemeral HTTP port per worker. The prod default (`8081`) collides
  across multiple workers and with the legacy control plane.
- The server + `entrypoint` live in `memory_bridge/worker.py` (an importable module), not
  `__main__`, because livekit's multiprocessing spawn pickles the entrypoint by import path
  (`Can't get attribute 'entrypoint' on __main__` otherwise). `__main__.py` only calls
  `cli.run_app(worker.server)`.
