## Why

Alfred is a LiveKit-based voice agent reachable only from a macOS Python client that runs as a launchd Aqua session on the Mac Mini (`mcj-mini-2-1`). There is no mobile client, so Alfred is unusable from an iPhone even though the tailnet already reaches the LiveKit server (`ws://100.69.175.118:7880`). LiveKit ships a fully working Swift/iOS voice-agent example, and a Mac with Xcode 26.6 + an Apple Developer account is available, so an iOS client is a low-risk, high-value addition.

## What Changes

- Add a native iOS (SwiftUI) client app for Alfred, forked from LiveKit's official Swift agent example as the starting point.
- Connect to the self-hosted LiveKit server over the tailnet (`ws://100.69.175.118:7880`, no TLS/TURN today).
- Publish microphone audio, subscribe to agent audio, and render the standard Alfred text streams: `lk.agent.ready` (cue-to-speak chime), `lk.agent.events` (`away` → end session), and `lk.transcription` (exit-keyword detection).
- Explicitly dispatch the active agent (`alfred-google-rt`) via LiveKit's agent-dispatch API, matching the existing client.
- Introduce a minimal token server to mint room-join tokens (today the Python client embeds API key/secret; iOS must not ship those in-app).
- Add a new `specs/ios-agent-client/spec.md` capability describing the client's required behavior.

## Capabilities

### New Capabilities

- `ios-agent-client`: Native iOS client that connects to Alfred over LiveKit, publishes mic audio, renders agent audio and text-stream state, dispatches the agent, and provides an authenticated token flow.

### Modified Capabilities

<!-- none: the existing repo has no specs (openspec/specs is empty) -->

## Impact

- **New code**: an iOS Xcode project (SwiftUI + LiveKit `livekit-client-sdk-swift`), a new token-server endpoint (extend `agent/admin.py` or add a small standalone service), and config plumbing for server URL / agent name.
- **Dependencies**: `livekit-client-sdk-swift` (SwiftPM), optionally `LiveKitComponents` for agent UI.
- **Systems**: LiveKit server (unchanged), agent worker `alfred-google-rt` (unchanged — the client is an additional participant), Tailscale (iOS client must be on the tailnet or a TLS/TURN path introduced later).
- **Security**: moves LiveKit credential usage out of the app and behind a token server.
