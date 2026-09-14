# Memory iOS Client

Native iOS voice client for talking to **Memory** (`memory-google-rt`) over the
self-hosted LiveKit infrastructure. Voice only: no camera, no screen share, no
text chat UI.

## Provenance

Forked from [`livekit-examples/agent-starter-swift`](https://github.com/livekit-examples/agent-starter-swift) (MIT).

- Upstream commit: `79ac0292dd8d691c13fa144a611184d7f977ccf7`
  (2026-08-04, "Add audio options panel with voice processing mode picker (#48)")
- Dependency pins are kept exactly as upstream (`Package.resolved`):
  - `livekit/client-sdk-swift` **2.16.0** (`79fb2beee98e45556bffebefa50b5d05c3382af1`)
  - `livekit/components-swift` **0.1.7** (`9f28db3ae5d2b51f3033ea725c952ddacb5657d0`)
- Targets: iOS 18.0+, Swift 6.0 (strict concurrency). Pure SwiftUI.

## What this app does

1. Reads connection settings from `UserDefaults` (see `MemoryConfig` below).
2. POSTs `{ identity, room, agent }` to the Memory token endpoint
   (`POST /api/token`, guarded by an `X-API-Key` header) and receives
   `{ server_url, participant_token }`.
3. Joins the room with a server-minted token. Agent dispatch is baked into the
   token server-side (`room_config.agents`), so the app never holds LiveKit
   credentials.
4. Publishes the mic with **echo cancellation enabled** from the first captured
   frame (`RoomOptions.defaultAudioCaptureOptions`); Memory uses Gemini-native
   activity detection (no server VAD), so client AEC is what makes barge-in work.
5. Consumes Memory's text-stream signals:
   - `lk.agent.ready` → cue-to-speak chime (system sound `1057` "Tink",
     configurable via `MemoryConfig.chimeSoundID`)
   - `lk.agent.events` → `user_state_changed` / `new_state == "away"` ends the
     session
   - `lk.transcription` → final *user* transcripts scanned for exit keywords
     (default `["goodbye"]`, configurable) end the session
6. Ends the session and returns to idle when the agent participant leaves.

## Configuration (`MemoryConfig`)

Stable, public API the settings screen binds to. All keys live in
`UserDefaults` (standard suite); property names are stable — do not rename.

| Property | UserDefaults key | Default |
|---|---|---|
| `serverURL` | `alfred.serverURL` | `ws://100.69.175.118:7880` |
| `agentName` | `alfred.agentName` | `memory-google-rt` |
| `tokenEndpoint` | `alfred.tokenEndpoint` | `http://100.69.175.118:8080/api/token` |
| `apiKey` | `alfred.apiKey` | `""` (must be set to match server `TOKEN_API_KEY`) |
| `exitKeywords` | `alfred.exitKeywords` | `["goodbye"]` |
| `agentConnectTimeout` | `alfred.agentConnectTimeout` | `20` (seconds) |
| `chimeSoundID` | `alfred.chimeSoundID` | `1057` |
| `participantIdentity` | `alfred.participantIdentity` | generated once per install (not for the settings UI) |

API: `MemoryConfig()` loads from `UserDefaults`; `config.save()` persists;
`MemoryConfig.reset()` restores build-time defaults. The configuration is read
once when `MemorySessionController` is created — the settings screen must
recreate the controller (or relaunch the app) after saving changes.

## Token endpoint contract

`POST {tokenEndpoint}` with header `X-API-Key: {apiKey}` and body:

```json
{ "identity": "...", "agent": "memory-google-rt" }
```

`room` is optional: when omitted the server derives a fresh per-token room
(`<agent>-ios-<hex8>`) and bakes it into the JWT grant, so the client never
manages a room name.

Response: `{ "server_url": "...", "participant_token": "..." }` (the SDK's
`TokenSourceResponse` shape). Non-2xx responses and unreachable endpoints are
surfaced as a "token fetch" failure with a Retry button.

## Project layout

- `VoiceAgent/VoiceAgentApp.swift` — `@main`, `AgentToConnect` (`.memory` case).
- `VoiceAgent/Memory/MemoryConfig.swift` — config + UserDefaults keys.
- `VoiceAgent/Memory/MemoryTokenSource.swift` — `EndpointTokenSource` for the
  Memory token endpoint.
- `VoiceAgent/Memory/MemorySessionController.swift` — session lifecycle, failure
  classification, signal handling (ready chime / away / exit keywords /
  agent-left).
- `VoiceAgent/Memory/MemoryFailureView.swift` — minimal failure + retry UI
  (a design pass will restyle it; state is a plain enum).
- Renaming policy: project/target/folder names keep the upstream `VoiceAgent`
  names; only the **user-visible** product name (`Memory`), bundle id
  (`com.emergent.memory`), and display name were changed, via `VoiceAgent.xcconfig`
  and `Info.plist`.

## Mac operator checklist (before first build)

Do these once in Xcode (or by editing `VoiceAgent.xcconfig`):

1. **Signing team** — set `DEVELOPMENT_TEAM` in
   `VoiceAgent/VoiceAgent.xcconfig` to your Apple Developer Team ID
   (currently an empty placeholder; simulator builds work without it).
2. **Bundle id** — optional: replace `com.emergent.memory` if your account needs a
   unique id (same file).
3. **Remove the `BroadcastExtension` target** (screen share — not used by the
   voice-only client). This was deliberately NOT hand-edited out of
   `project.pbxproj` (error-prone without a build to verify). In Xcode:
   select `BroadcastExtension` in the project navigator → Delete → "Remove
   Reference" (or "Delete Files" — either works since the scheme no longer
   references it for builds). The `Embed Foundation Extensions` build phase and
   the `PBXTargetDependency` are removed automatically with the target.
   While it exists the target is harmless: the screen-share UI is disabled in
   Swift and the extension's team/bundle were already updated
   (`com.emergent.memory.broadcast`).
4. **Scheme/product name** — the shared scheme `VoiceAgent.xcscheme` was
   already updated to `Memory.app` (matching `PRODUCT_NAME = Memory`). If Xcode
   ever reports a stale buildable name, delete the derived scheme and let Xcode
   regenerate it.
5. **Build & run** — open `VoiceAgent.xcodeproj`, select the `VoiceAgent`
   scheme, and run on a physical iPhone (voice needs a real mic + Tailscale on
   the device; see below).

Before talking to Memory, set the API key in the app (settings screen or
`MemoryConfig`) to match the server's `TOKEN_API_KEY`, and confirm the token
endpoint is reachable from the device.

## Notes & trade-offs

- **Transport**: `ws://100.69.175.118:7880` over Tailscale, no TLS. The
  `Info.plist` carries `NSAppTransportSecurity → NSAllowsLocalNetworking` (the
  narrow exception; no `NSAllowsArbitraryLoads`) and
  `NSLocalNetworkUsageDescription`. UDP 50000–60000 must be reachable (WebRTC);
  prefer Wi-Fi.
- **Microphone permission**: exactly one authoritative string —
  `NSMicrophoneUsageDescription` in `VoiceAgent/Info.plist` (the upstream
  `INFOPLIST_KEY_NSMicrophoneUsageDescription` build setting was removed from
  `project.pbxproj` to avoid a conflict). Denials surface via the SDK as a
  "mic permission" failure with retry.
- **Transcription scanning** consumes the SDK `Session`'s transcription message
  history (`lk.transcription` is registered by `Session` itself, which forbids a
  second handler on the same topic).
- **dSYM warning** on archive (LiveKitWebRTC ships without dSYMs) is expected
  and does not block submission.
- **No RPC methods, no room metadata**: Memory's protocol is text-streams only;
  nothing here calls RPC or reads room attributes.
