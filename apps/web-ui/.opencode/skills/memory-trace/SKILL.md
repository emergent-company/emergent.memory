---
name: memory-trace
description: Trace a Memory session end-to-end (iOS app + bridge worker) by room. Use when a voice/text session misbehaves and you need to see exactly what the app did and where the worker replied (or failed).
---

# Memory Trace

## When to use

- A session gets no response / wrong response / drops mid-turn.
- Need the full journey: tap → token → LiveKit → agent → LLM → TTS → transcription → failure.

## Quick start

```bash
tools/memory-trace.sh <room>
```

Merges app trace + worker trace, sorted by time, one line per event.

## Find the room

- Ask the user, OR
- `ls -t /tmp/memory-trace/` (latest worker rooms), OR
- read the app trace tail — the `room` field appears once the token is fetched.

## Where traces live

- **Worker**: `/tmp/memory-trace/<room>.jsonl` on the dev server (env `MEMORY_TRACE_DIR`). Read directly: `jq . /tmp/memory-trace/<room>.jsonl`.
- **App**: simulator container `Documents/memory-trace.jsonl` on `mcj-mini`.
  ```bash
  CONT=$(ssh mcj-mini "xcrun simctl get_app_container booted com.emergent.memory data")
  ssh mcj-mini "cat $CONT/Documents/memory-trace.jsonl" | jq .
  ```

## Event vocabulary

- **Worker**: `bridge_starting`, `bridge_ready`, `text_input_received`, `conversation_item_added`, `user_state_changed`, `closing_session`, `error` (+ `llm_stream_start`, `llm_done`, `llm_error`).
- **App**: `agent_selected`, `connect_tapped`, `retry_tapped`, `phase_changed`, `token_fetch_start`, `token_fetched`, `token_fetch_error`, `livekit_connected`, `agent_joined`, `agent_left`, `mic_toggled`, `chat_mode_entered`, `voice_mode_entered`, `text_sent`, `hangup_tapped`, `message_received`, `failure`, `session_ended`.

## Diagnostic checklist (read the merged trace top to bottom)

1. App `connect_tapped` → `token_fetch_start` → `token_fetched` (has `room`, `server_url`) — token ok?
2. App `livekit_connected` → `agent_joined` — room + agent joined?
3. Worker `bridge_starting`/`bridge_ready` — did THIS worker handle the dispatch (vs a prod worker)?
4. App `text_sent` → worker `text_input_received` — text reached the worker?
5. Worker `conversation_item_added` (`role=assistant`) — LLM produced a reply?
6. App `message_received` (agent transcript) — reply came back to the app?
7. `failure` / `error` / `closing_session` — where it broke.

Correlate by `room` + `ts`.
