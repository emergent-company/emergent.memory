## Context

See proposal.md for motivation. Constraints that shape the approach:

- Alfred's agent stack: LiveKit server `v1.9.2` at `ws://100.69.175.118:7880` (Tailscale-only, **no TLS**, no TURN), worker `alfred-google-rt` on `livekit-agents 1.6.9`, **explicit dispatch** (agent does not auto-join; the client must dispatch it).
- The existing macOS client mints tokens **in-app** from `LIVEKIT_API_KEY`/`LIVEKIT_API_SECRET` — unacceptable to ship in an iOS binary, and there is **no token server today**.
- Alfred's text streams: `lk.agent.ready` (cue-to-speak), `lk.agent.events` (`user_state_changed` → `away`), `lk.transcription` (user transcript exit keywords). No RPC methods, no data-channel messages, no room attributes/metadata.
- Build host: Mac Mini `mcj-mini-2-1` (macOS 26.4.1, Xcode 26.6, iOS 26.x simulators, Apple Developer account).
- Agent-side interop confirmed compatible: `livekit-agents >= 1.0.0` emits `lk.agent.state`, `lk.transcription` streams; `alfred-google-rt` uses Gemini-native activity detection (no server VAD), so client-side echo cancellation matters for barge-in.

## Goals / Non-Goals

**Goals:**

- A SwiftUI iOS app forked from `livekit-examples/agent-starter-swift` that talks to Alfred: connect → dispatch `alfred-google-rt` → mic publish (AEC on) → agent audio playback → honor ready/away/exit-keyword signals.
- A minimal token server on Alfred's existing infrastructure that mints room-join tokens server-side, so no LiveKit secret lives in the app.
- Configurable server URL + agent name + token endpoint without recompiling (build-settings or runtime settings screen).

**Non-Goals:**

- Voice **wake word** on-device (the macOS client uses an ONNX `hey_alfred` model; iOS starts from a tap-to-talk affordance — wake word is a future enhancement).
- Camera/video, screen-share (drop `BroadcastExtension`), text chat UI.
- TLS/TURN on the LiveKit server (iOS client rides the existing Tailscale-only `ws://` path for now; ATS exception + local-network permission handle the non-TLS transport).
- Modifying the agent worker (`alfred-google-rt`) or LiveKit server.
- Memory-MCP integration on the client (agent already exposes it server-side).

## Decisions

### D1 — Fork `agent-starter-swift` as the base

**Choice:** Clone `livekit-examples/agent-starter-swift` (MIT, Xcode 26.2, Swift 6.0, iOS 18.0+) as the starting project; keep LiveKit's SwiftUI `Session`/`LocalMedia` architecture rather than hand-rolling `Room()` calls.

**Why:** The starter already implements mic publish + AEC options, agent-state parsing (`lk.agent.state`), transcription/text-stream handlers, agent-connect timeout, and pre-connect audio buffering. It is the officially maintained example and matches Alfred's `livekit-agents >= 1.0.0` protocol exactly.

**Alternatives:** (a) Start from bare `Room()` + `livekit-client-sdk-swift` — full control but re-implements the agent lifecycle glue the starter already has. (b) Start from the older `voice-assistant-swift` — dead (301 redirects to the starter). Rejected.

### D2 — Custom `EndpointTokenSource` pointed at a new Alfred token server

**Choice:** Add an `AgentToConnect` case implementing `EndpointTokenSource` (POST `TokenRequestOptions`, decode `TokenSourceResponse { server_url, participant_token }`). Add a new token endpoint to Alfred's existing `agent/admin.py` `ThreadingHTTPServer` (port 8080) rather than a new service.

**Why:** `EndpointTokenSource` is the SDK's first-class self-hosting path (the starter's `HomepageTokenSource` is the working example). Extending `agent/admin.py` keeps the token minting beside the agent's other HTTP surface, reuses `LIVEKIT_API_KEY`/`LIVEKIT_API_SECRET` from `.env`, and avoids deploying a new process. Server-side minting keeps secrets out of the iOS binary.

**Alternatives:** (a) Embed keys in-app — rejected (secret exposure). (b) Standalone FastAPI service — more moving parts for one route. (c) LiveKit Cloud sandbox token server — cloud-only, does not reach the self-hosted server.

**Token contract:** `POST /api/token` with `{ identity, room }` → `{ server_url, participant_token }`. Endpoint validates against a configured allowed agent/room and mints `VideoGrants(room_join=True, room=room)` with `with_identity(...)`, mirroring the macOS client's grant shape.

### D3 — Explicit agent dispatch via `room_config.agents`

**Choice:** Dispatch `alfred-google-rt` by having the token server embed `room_config.agents` in the JWT (agent name from the request), so the Swift `Session`/`TokenSourceConfigurable` path (`agentName`) triggers dispatch on connect.

**Why:** Alfred's agent is explicit-dispatch; the SDK's `TokenSourceConfigurable` already carries `agentName`/`agentDeployment` and encodes it as `room_config.agents`. Routing dispatch through the token keeps the app dumb (no direct LiveKit HTTP API calls from iOS) and matches the spec's "server-minted token" model.

**Alternative:** App calls LiveKit's agent-dispatch HTTP API directly (like the Python client) — requires the app to hold LiveKit HTTP API access; rejected for the same secret-hygiene reason as D2.

### D4 — Transport: Tailscale `ws://`, ATS exception for the CGNAT range

**Choice:** Connect to `ws://100.69.175.118:7880` over the user's existing Tailscale. Add `NSAppTransportSecurity` → `NSExceptionDomains` → `100.64.0.0/10` → `NSExceptionAllowsInsecureHTTPLoads = true` (a CIDR-scoped exception), plus `NSLocalNetworkUsageDescription` for the LAN fallback.

**Why (revised during implementation):** The original plan used `NSAllowsLocalNetworking`. That proved wrong on iOS 26 in two ways: (1) `NSAllowsLocalNetworking` does **not** cover `100.64.0.0/10` (Tailscale CGNAT is RFC 6598 shared space, not RFC1918), so plain `http://`/`ws://` to the tailnet IP was still ATS-blocked; (2) `NSAllowsArbitraryLoads` is **silently ignored** when `NSAllowsLocalNetworking` is also present (Apple-documented), so adding it alongside did nothing. The working fix is a `NSExceptionDomains` CIDR entry for `100.64.0.0/10` — the iOS 17+ mechanism for IP/CIDR exceptions, scoped rather than blanket.

**TLS note:** Tailscale can mint free Let's Encrypt certs for `livekit.tail0358fa.ts.net` (`tailscale cert`), but LiveKit server **v1.9.2 has no native TLS config** (`rtc` section has no `tls_cert`/`tls_key`). Real TLS (wss/https) therefore needs a reverse proxy (caddy) in front of LiveKit + the token endpoint. Recorded as a future hardening item; the ATS CIDR exception is the working transport today.

### D5 — Configuration via a runtime settings screen (not `.xcconfig`)

**Choice:** Store server URL, agent name, and token-endpoint URL in a small settings screen backed by `UserDefaults` (with build-time defaults), consumed by the token-source case.

**Why:** The starter's `.xcconfig` only holds bundle id/team; connection config lives in code. Runtime config lets the same build point at different tailnet hosts without recompiling and without shipping secrets.

**Alternative:** Hardcode via a Swift enum like the starter — simpler but requires a rebuild per environment. Rejected.

## Risks / Trade-offs

- **Non-TLS `ws://` transport** → ATS local-networking exception + Tailscale-only access; acceptable for a personal agent. Mitigation: scope ATS narrowly; track TLS+TURN as future work.
- **No TURN → UDP (50000–60000) must reach the phone over Tailscale** → Some cellular/NAT paths may still fail. Mitigation: confirm Tailscale routes UDP on the tailnet; fall back to documenting Wi-Fi-first usage.
- **LiveKit WebRTC ships without dSYMs** → cosmetic App Store/TestFlight archive warning; does not block submission. Mitigation: accept the warning (documented in starter README).
- **Gemini-native activity detection (no server VAD)** → barge-in quality depends on client AEC. Mitigation: enable `echoCancellationMode` on mic capture from day one.
- **Token server on `agent/admin.py` (port 8080) currently has no auth** → adding an unauthenticated token route would let anyone on the tailnet mint room-join tokens. Mitigation: gate `/api/token` behind a shared token/`X-API-Key` header checked against `.env`, and document the tailnet-only trust boundary.
- **Starter repo drift (Swift 6 strict concurrency)** → forking pulls in Swift 6 mode; minor build friction. Mitigation: keep the fork pinned via `Package.resolved`; adjust concurrency annotations only where required.

## Migration Plan

1. Add `/api/token` to `agent/admin.py` behind an API-key header; restart the admin server.
2. Add the forked iOS app; configure server URL/agent/token endpoint; build + run on the iOS simulator against the tailnet IP.
3. Physical device: set `DEVELOPMENT_TEAM` + unique `PRODUCT_BUNDLE_IDENTIFIER` in `VoiceAgent.xcconfig`, enable Developer Mode, automatic signing.
4. No rollback needed (additive; agent + LiveKit server untouched). The old macOS client continues to work unchanged.

## Open Questions

- Whether to pursue a TLS/TURN path for non-Tailscale (public) access — deferrable; does not change the spec or task breakdown.
- Whether on-device wake word (porting the ONNX `hey_alfred` model) is desired later — deferrable enhancement.
