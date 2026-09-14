# 06 — LiveKit

LiveKit is the **voice transport only** — real-time audio in/out and room management. It
holds no agent intelligence, no STT/TTS, no state.

## Role

- WebRTC signaling + media transport between clients and bridge workers.
- **Agent dispatch:** `roomConfig.agents = [RoomAgentDispatch(agent_name)]` — when a client
  joins a room, LiveKit dispatches the matching worker process into it.

## Server

- **Self-hosted**, external to the Memory Compose (D3). Currently home2 LXC 112:
  - signaling `ws://<tailnet>:7880` · RTC TCP `7881` · RTC media UDP `50000–60000`
  - `rtc.node_ip` = Tailscale IP (required for cross-network media)
  - no TURN — all endpoints on the tailnet.
- Memory connects to it as a client (`LIVEKIT_URL`). It is not managed by the Go supervisor.

## Rooms & dispatch

- Room naming: `<agent_name>-<client>-<nonce>` (e.g. `memory-ios-8f3a`, `diane-mac-1c2d`).
  Token allow-list matches the `<agent_name>-` prefix.
- `agent_name` == the agent's `name` in memory == the worker's `AGENT_NAME`. Three-way
  identity keeps dispatch, memory, and the process aligned.

## Worker readiness

Dispatch only succeeds once the bridge worker is **registered with LiveKit** (its
`AgentServer` connected). Two timing facts matter:

1. **Cold start:** a freshly spawned worker takes ~1–2s to connect and register. A client
   joining before registration gets a "no agent available" failure → the supervisor should
   prefer **pre-warmed workers** for known agents (spawn at agent-create time, keep idle).
2. **Ad-hoc agent:** creating an agent and immediately calling it may hit the cold-start
   window. The client surfaces "agent starting…" and retries; the supervisor warms the worker
   on create (async, seconds).

```
iOS/Mac client ── join room(agent_name) ──► LiveKit ── dispatch ──► bridge worker(agent_name)
                                                                        │
                                                                        └─► memory chat (same agent)
```

## Token minting

Moved to the Go app (from retired `admin.py`). Shared by iOS and browser clients.

1. Client POSTs `{ identity, room?, agent?, client? }` to Go `POST /api/token`.
2. Go app mints a short-lived room-join JWT using server-side LiveKit API key/secret,
   embedding `video.roomJoin`, `video.room`, identity, and `roomConfig.agents` dispatch.
   When `room` is omitted it is derived as `<agent>-<client>-<8hex>`; `client` defaults
   to `web` (iOS passes `client:"ios"` explicitly to keep `<agent>-ios-...`).
3. Response `{ server_url, participant_token }`.

Room allow-list + `X-API-Key` gate preserved (see 09-security.md).

## Browser voice

- The web UI (`/chat`) now offers a voice call button. `voice.js` + the vendored
  `livekit-client` UMD build (`static/js/vendor/livekit-client.umd.js`) drive the call:
  fetch token → connect → publish mic → attach agent audio on `TrackSubscribed`. Same
  `roomConfig.agents` dispatch as native — no server-sdk/webhook changes.
- Secure-context requirement: the page must be served over HTTPS on a Tailscale
  MagicDNS name (`*.ts.net`), not a raw `100.x.y.z` IP, and `LIVEKIT_PUBLIC_URL` must be
  `wss://`/`https://` — otherwise the browser hits mixed-content or cert-hostname
  failures, and `getUserMedia` is unavailable on non-secure origins.

## Failure model

- Worker process down → LiveKit dispatch fails → client surfaces retry (supervisor restarts
  the worker independently).
- LiveKit down → voice unavailable; web text chat unaffected (chat does not touch LiveKit).
